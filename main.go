// Copyright (c) 2022 Tailscale Inc & AUTHORS All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// tbr-audit is a GitHub app which will walk through recently merged
// pull requests from a GitHub repository and validate that at least one
// reviewer approved them. If a Pull Request was merged without a reviewer,
// a GitHub issue will be filed requesting a followup review.
//
// This bot is intended to provide a control for SOC2 while allowing
// to-be-reviewed changes to be submitted.
package main

import (
	"context"
	"expvar"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/bradleyfalzon/ghinstallation/v2"
	"github.com/google/go-github/v71/github"
	"go4.org/mem"
	"tailscale.com/tsweb"
)

var (
	reposChecked   = expvar.NewInt("tbrbot_repos_checked")
	totalWakeups   = expvar.NewInt("tbrbot_total_wakeups")
	webhookWakeups = expvar.NewInt("tbrbot_webhook_wakeups")
)
var wakeBot chan int
var gitHubWebhookSecret []byte

type githubInfo struct {
	org     string
	bugrepo string
	appname string
	repos   string

	// secrets
	appId         int64
	appInstall    int64
	appPrivateKey []byte
}

// Return an HTTP client suitable to use with the GitHub API, initialized with
// our API keys and certificate.
//
// This bot expects to run as an organization-level GitHub app, as seen in
// https://github.com/organizations/<name>/settings/installations
// This gives it permission to access private repos without using an individual's
// Personal Access Token.
func getGitHubApiClient(args githubInfo) *github.Client {
	itr, err := ghinstallation.New(http.DefaultTransport, args.appId,
		args.appInstall, args.appPrivateKey)
	if err != nil {
		log.Fatal(err)
	}
	return github.NewClient(&http.Client{Transport: itr})
}

// check whether an issue has already been filed for the given PR, and file one if not.
func fileFollowupIssue(ctx context.Context, client *github.Client, repo string, args githubInfo,
	prNum int) error {
	title := fmt.Sprintf("TBR %s/%s/pull/%d followup review", args.org, repo, prNum)

	// all of the followup issues are in the bugrepo, no matter the repo of the submitted PR
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
			if review.Body != nil {
				if mem.ContainsFold(mem.S(*review.Body), mem.S("LGTM")) {
					return true
				}

				// https://twitter.com/naomi_lgbt/status/1573462393103192064
				if mem.ContainsFold(mem.S(*review.Body), mem.S("banger pr")) {
					return true
				}

				for _, emoji := range [...]string{"🚢", ":shipit:"} {
					if mem.EqualFold(mem.S(strings.TrimSpace(*review.Body)), mem.S(emoji)) {
						return true
					}
				}
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
		Author    string
		MergedBy  string
	}
	pulls := make(map[int]*PullRequestState, 50)

	opt := &github.ListOptions{PerPage: 100}
	ctx := context.Background()
	for {
		// ListRepositoryEvents returns the most recent 300 events, but only
		// those occurring in the last 90 days. It is recommended this bot be
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
				pulls[prNum].Author = *payload.PullRequest.User.Login
				if payload.PullRequest.MergedBy != nil {
					pulls[prNum].MergedBy = *payload.PullRequest.MergedBy.Login
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
				pulls[prNum].Author = *payload.PullRequest.User.Login
				if payload.PullRequest.MergedBy != nil {
					pulls[prNum].MergedBy = *payload.PullRequest.MergedBy.Login
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
			// for dependabot, our policy is that merging the PR is Approval.
			if pr.Author == "dependabot[bot]" {
				continue
			}

			// merging someone else's PR indicates approval of that PR.
			if pr.MergedBy != "" && pr.Author != pr.MergedBy {
				continue
			}

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

func handleWebhook(w http.ResponseWriter, r *http.Request) {
	payload, err := github.ValidatePayload(r, gitHubWebhookSecret)
	if err != nil {
		log.Printf("error validating request body: err=%s\n", err)
		return
	}
	defer r.Body.Close()

	event, err := github.ParseWebHook(github.WebHookType(r), payload)
	if err != nil {
		log.Printf("could not parse webhook: err=%s\n", err)
		return
	}
	_ = event

	switch event.(type) {
	case *github.PullRequestReviewEvent:
	case *github.PullRequestEvent:

	default:
		// not something we need to respond to
		return
	}

	select {
	case wakeBot <- 1:
	default:
		// Somebody else already woke the bot. Just return.
	}
}

func processArgs() githubInfo {
	var args githubInfo
	args.org = os.Getenv("TBRBOT_ORG")
	args.bugrepo = os.Getenv("TBRBOT_BUGREPO")
	args.appname = os.Getenv("TBRBOT_APPNAME")
	appIdString := os.Getenv("TBRBOT_APP_ID")
	appInstallString := os.Getenv("TBRBOT_APP_INSTALL")
	args.repos = os.Getenv("TBRBOT_REPOS")

	if args.org == "" || args.bugrepo == "" || args.appname == "" || appIdString == "" || appInstallString == "" {
		log.Fatal("TBRBOT_ORG, TBRBOT_BUGREPO, TBRBOT_APPNAME, TBRBOT_APP_ID, and TBRBOT_APP_INSTALL are required environment variables")
	}

	var err error
	args.appId, err = strconv.ParseInt(appIdString, 10, 64)
	if err != nil {
		log.Fatalf("Cannot parse TBRBOT_APP_ID as integer: %q", appIdString)
	}
	args.appInstall, err = strconv.ParseInt(appInstallString, 10, 64)
	if err != nil {
		log.Fatalf("Cannot parse TBRBOT_APP_INSTALL as integer: %q", appInstallString)
	}

	if args.repos == "" {
		log.Print("WARNING: no TBRBOT_REPOS set, no GitHub repositories are being monitored")
	}

	args.appPrivateKey = []byte(os.Getenv("TBRBOT_APP_PRIVATE_KEY"))
	gitHubWebhookSecret = []byte(os.Getenv("WEBHOOK_SECRET"))

	return args
}

func mainLoop(args githubInfo) {
	ticker := time.NewTicker(1 * time.Hour)
	client := getGitHubApiClient(args)

	for {
		totalWakeups.Add(1)

		// check for to-be-reviewed PRs once at startup,
		// and then whenever something wakes us up.
		for _, repo := range strings.Split(args.repos, ",") {
			reposChecked.Add(1)
			checkForToBeReviewed(client, repo, args)
		}

		select {
		case <-ticker.C:
			log.Print("Periodic check of repositories for TBR submissions.")
		case <-wakeBot:
			// A webhook will wake the bot immediately after submission. We pause
			// a generous amount of time before checking, for two reasons:
			// 1. If they realize they didn't have an approval, give them a few
			//    minutes to find someone to review it. An Approval or LGTM comment
			//    added before the bot runs will suffice.
			// 2. If someone submits and didn't even realize there was no approval,
			//    it is disconcerting to have the bot file an issue the instant one's
			//    finger lifts from the mouse button. Wait a decent interval before
			//    proceeding.
			log.Print("Webhook notification received, pausing before checking repositories.")
			time.Sleep(5 * time.Minute)
			webhookWakeups.Add(1)
		}
	}
}

func main() {
	log.Print("ToBeReviewedBot is starting")
	args := processArgs()
	wakeBot = make(chan int)

	go mainLoop(args)

	mux := http.NewServeMux()
	tsweb.Debugger(mux)
	mux.HandleFunc("/webhook", handleWebhook)
	srv := &http.Server{
		Addr:    ":8080",
		Handler: mux,
	}
	log.Fatal(srv.ListenAndServe())
}
