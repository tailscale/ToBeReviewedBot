// Copyright (c) 2022 Tailscale Inc & AUTHORS All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// tbr-audit is a Github action which will walk through recently merged
// pull requests from a GitHub repository and validate that at least one
// reviewer approved them. If a Pull Request was merged without a reviewer,
// a GitHub issue will be filed requesting a followup review.
//
// This bot is intended to provide a control for SOC2 CC 5.2-04 while
// allowing to-be-reviewed changes to be submitted.
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/bradleyfalzon/ghinstallation/v2"
	"github.com/google/go-github/v42/github"
)

type githubInfo struct {
	org     string
	bugrepo string
	appname string
}

func mustInt64(env string) int64 {
	i, err := strconv.ParseInt(os.Getenv(env), 10, 64)
	if err != nil {
		log.Fatalf("Invalid %s environment variable, must be integer", env)
	}

	return i
}

// Return an HTTP client suitable to use with the GitHub API, initialized with
// our API keys and certificate.
//
// This bot expects to run as an organization-level GitHub app, as seen in
// https://github.com/organizations/<name>/settings/installations
// This gives it permission to access private repos without using an individual's
// Personal Access Token.
func getGithubApiClient() *github.Client {
	appID := mustInt64("GH_APP_ID")
	appInstall := mustInt64("GH_APP_INSTALL_ID")
	key := []byte(os.Getenv("GH_APP_PRIVATE_KEY"))

	itr, err := ghinstallation.New(http.DefaultTransport, appID, appInstall, key)
	if err != nil {
		log.Fatal(err)
	}
	return github.NewClient(&http.Client{Transport: itr})
}

// check whether an issue has already been filed for the given PR, and file one if not.
func fileFollowupIssue(ctx context.Context, client *github.Client, repo string, args githubInfo,
	prNum int) error {
	title := fmt.Sprintf("TBR %s/%s/pull/%d followup review", args.org, repo, prNum)

	// all of the followup issues are in corp, no matter the repo of the submitted PR
	check := fmt.Sprintf("%s in:title repo:%s/%s author:app/%s", title, args.org,
		args.bugrepo, args.appname)
	followup, _, err := client.Search.Issues(ctx, check, &github.SearchOptions{
		ListOptions: github.ListOptions{PerPage: 10}})
	if err != nil {
		return err
	}

	if len(followup.Issues) > 0 {
		// Issue already filed, nothing more to do.
		return nil
	}

	body := fmt.Sprintf("https://github.com/%s/%s/pull/%d was filed to-be-reviewed without any reviewer approving. Someone needs to review it, followup on any changes needed, and note completion by closing this issue.", args.org, repo, prNum)
	req := github.IssueRequest{Title: &title, Body: &body}
	_, _, err = client.Issues.Create(ctx, args.org, args.bugrepo, &req)
	if err != nil {
		return err
	}

	return nil
}

// The Activity API only includes State=APPROVED if the approval came within the last
// 300 events. PRs approved a long time before submission will not show as APPROVED
// in the PullRequestReviewEvent.
// To double-check, we use the API to check all comments of a Pull Request if any of them
// contained an Approval.
func wasPrEverApproved(client *github.Client, repo string, args githubInfo, prNum int) bool {
	opt := &github.ListOptions{PerPage: 100}
	ctx := context.Background()
	for {
		reviews, resp, err := client.PullRequests.ListReviews(ctx, args.org, repo, prNum, opt)
		if err != nil {
			log.Fatal(err.Error())
		}

		for _, review := range reviews {
			if strings.EqualFold(*review.State, "approved") {
				return true
			}
		}

		if resp.NextPage == 0 {
			break
		}
		opt.Page = resp.NextPage
	}

	return false
}

func checkForToBeReviewed(client *github.Client, repo string, args githubInfo) {
	type PullRequestState struct {
		Approved  bool
		Submitted bool
	}
	pulls := make(map[int]*PullRequestState, 50)

	opt := &github.ListOptions{PerPage: 100}
	ctx := context.Background()
	for {
		// ListRepositoryEvents returns the most recent 300 events, but only
		// those occuring in the last 90 days. It is recommended this bot be
		// run at least once per day and at most every 15 minutes, depending
		// on how much activity there is, in order to not miss events.
		events, resp, err := client.Activity.ListRepositoryEvents(ctx, args.org, repo, opt)
		if err != nil {
			log.Fatal(err.Error())
		}

		for _, evt := range events {
			typ := evt.GetType()
			if typ == "PullRequestEvent" {
				p, err := evt.ParsePayload()
				if err != nil {
					log.Printf("failed (%v) to parse: %v\n", err, evt)
					continue
				}
				payload := p.(*github.PullRequestEvent)
				prNum := *payload.PullRequest.Number
				if _, ok := pulls[prNum]; !ok {
					pulls[prNum] = &PullRequestState{}
				}
				if strings.EqualFold(*payload.Action, "closed") &&
					*payload.PullRequest.Merged {
					pulls[prNum].Submitted = true
				}
			}
			if typ == "PullRequestReviewEvent" {
				p, err := evt.ParsePayload()
				if err != nil {
					log.Printf("failed (%v) to parse: %v\n", err, evt)
					continue
				}
				payload := p.(*github.PullRequestReviewEvent)
				prNum := *payload.PullRequest.Number
				if _, ok := pulls[prNum]; !ok {
					pulls[prNum] = &PullRequestState{}
				}
				if strings.EqualFold(*payload.Review.State, "approved") {
					pulls[prNum].Approved = true
				}
			}
		}

		if resp.NextPage == 0 {
			break
		}
		opt.Page = resp.NextPage
	}

	for prNum, pr := range pulls {
		if pr.Submitted && !pr.Approved {
			// Double-check if there was ever an approval.
			if wasPrEverApproved(client, repo, args, prNum) {
				continue
			}

			// This Pull Request was submitted without an Approver.
			err := fileFollowupIssue(ctx, client, repo, args, prNum)
			if err != nil {
				log.Fatal(err)
			}
		}
	}
}

func main() {
	var args githubInfo
	args.org = os.Getenv("TBRBOT_ORG")         // GitHub organization to use
	args.bugrepo = os.Getenv("TBRBOT_BUGREPO") // name of repository to file followup issues in
	args.appname = os.Getenv("TBRBOT_APPNAME") // The AppSlug of the GH App running the bot

	// comma-separated list of GitHub repositories to check for to-be-reviewed PRs
	repos := os.Getenv("TBRBOT_REPOLIST")

	if args.org == "" || args.bugrepo == "" || args.appname == "" || repos == "" {
		log.Fatal("TBRBOT_ORG, TBRBOT_BUGREPO, TBRBOT_APPNAME, and TBRBOT_REPOLIST must be set as repository secrets for Actions runners")
	}

	client := getGithubApiClient()
	for _, repo := range strings.Split(repos, ",") {
		checkForToBeReviewed(client, repo, args)
	}
}
