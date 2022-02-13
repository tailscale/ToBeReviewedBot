package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/bradleyfalzon/ghinstallation/v2"
	"github.com/google/go-github/v42/github"
)

var org string = "example-github-organization"

// Return an HTTP client suitable to use with the GitHub API, initialized with
// our API keys and certificate.
//
// This bot expects to run as an organization-level GitHub app, as seen in
// https://github.com/organizations/<name>/settings/installations
// This gives it permission to access private repos without using an individual's
// Personal Access Token.
func getGithubApiClient() *github.Client {
	app_id_string := os.Getenv("GH_APP_ID")
	app_install_string := os.Getenv("GH_APP_INSTALL_ID")
	key_string := os.Getenv("GH_APP_PRIVATE_KEY")

	if app_id_string == "" || app_install_string == "" || key_string == "" {
		log.Fatalf("GH_APP_ID, GH_APP_INSTALL_ID, and GH_APP_PRIVATE_KEY env variables must be set")
	}

	app_id, err := strconv.ParseInt(app_id_string, 10, 64)
	if err != nil {
		log.Fatal("Invalid GH_APP_ID environment variable, must be integer")
	}
	app_install, err := strconv.ParseInt(app_install_string, 10, 64)
	if err != nil {
		log.Fatal("Invalid GH_APP_INSTALL_ID environment variable, must be integer")
	}
	key := []byte(key_string)

	itr, err := ghinstallation.New(http.DefaultTransport, app_id, app_install, key)
	if err != nil {
		log.Fatal(err)
	}
	return github.NewClient(&http.Client{Transport: itr})
}

// check whether an issue has already been filed for the given PR, and file one if not.
func fileFollowupIssue(ctx context.Context, client *github.Client, repo, bugrepo string, prNum int) error {
	title := fmt.Sprintf("TBR %s/%s/pull/%d followup review", org, repo, prNum)

	// all of the followup issues are in corp, no matter the repo of the submitted PR
	check := fmt.Sprintf("%s in:title repo:%s/%s", title, org, bugrepo)
	followup, _, err := client.Search.Issues(ctx, check, &github.SearchOptions{
		ListOptions: github.ListOptions{PerPage: 10}})
	if err != nil {
		return err
	}

	if len(followup.Issues) > 0 {
		// Issue already filed, nothing more to do.
		return nil
	}

	body := fmt.Sprintf("https://github.com/%s/%s/pull/%d was filed to-be-reviewed "+
		"without any reviewer approving. Someone needs to review it, followup on any "+
		"changes needed, and note completion by closing this issue.", org, repo, prNum)
	req := github.IssueRequest{Title: &title, Body: &body}
	_, _, err = client.Issues.Create(ctx, org, bugrepo, &req)
	if err != nil {
		return err
	}

	return nil
}

func payloadOrDie(event *github.Event) interface{} {
	payload, err := event.ParsePayload()
	if err != nil {
		log.Fatalf("failed (%v) to parse: %v\n", err, event)
	}
	return payload
}

// The Activity API only includes State=APPROVED if the approval came within the last
// 300 events. PRs approved a long time before submission will not show as APPROVED
// in the PullRequestReviewEvent.
// To double-check, we use the API to check all comments of a Pull Request if any of them
// contained an Approval.
func wasPrEverApproved(client *github.Client, repo string, prNum int) bool {
	opt := &github.ListOptions{PerPage: 100}
	ctx := context.Background()
	for {
		reviews, resp, err := client.PullRequests.ListReviews(ctx, org, repo, prNum, opt)
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

func checkForToBeReviewed(client *github.Client, repo, bugrepo string) {
	type PullRequestState struct {
		Approved  bool
		Submitted bool
	}
	pulls := make(map[int]*PullRequestState, 50)

	opt := &github.ListOptions{PerPage: 100}
	ctx := context.Background()
	for {
		events, resp, err := client.Activity.ListRepositoryEvents(ctx, org, repo, opt)
		if err != nil {
			log.Fatal(err.Error())
		}

		for _, evt := range events {
			typ := evt.GetType()
			if typ == "PullRequestEvent" {
				payload := payloadOrDie(evt).(*github.PullRequestEvent)
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
				payload := payloadOrDie(evt).(*github.PullRequestReviewEvent)
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
			if wasPrEverApproved(client, repo, prNum) {
				continue
			}

			// This Pull Request was submitted without an Approver.
			err := fileFollowupIssue(ctx, client, repo, bugrepo, prNum)
			if err != nil {
				log.Fatal(err)
			}
		}
	}
}

func main() {
	flag.StringVar(&org, "org", "example-github-organization", "GitHub organization to use")
	reposPtr := flag.String("repos", "example-github-repo",
		"comma-separated list of GitHub repositories to check for to-be-reviewed PRs")
	bugrepoPtr := flag.String("bugrepo", "ToBeReviewedBot",
		"name of repository to file followup issues in")
	flag.Parse()

	client := getGithubApiClient()
	for _, repo := range strings.Split(*reposPtr, ",") {
		checkForToBeReviewed(client, repo, *bugrepoPtr)
	}
}
