## ToBeReviewed Bot

The automation in this repository supports a To-Be-Reviewed Pull Request workflow:
+ Allows a repository to enable branch protection and require pull
  requests, but have flexibility in submission of pull requests in
  case of urgent need by not mandating an approver before submission.
+ If a PR is submitted without an Approver, the bot will notice
  within a few minutes and file a GitHub issue requiring followup.
+ The bot notes cases where intent is clear and does not intervene. Merging
  someone else's PR constitutes Approval. A comment containing "LGTM"
  constitutes Approval.
+ The issues requiring followup carry a distinctive title allowing for
  easy generation of the full population during a Compliance-related
  periodic audit, and to demonstrate that all such issues did get
  a followup review within a reasonable amount of time.


### Configuration variables
The bot expects to run continuously on a production system, and
supports the following environment variables:
- `TBRBOT_ORG`: The GitHub organization to use, like "tailscale".
- `TBRBOT_BUGREPO`: The name of the repository to file followup issues in, like "private".
- `TBRBOT_APPNAME`: The name of the GitHub app account, like "tbr-bot", to use as the
  author in issues filed.
- `TBRBOT_REPOS`: A comma-separated list of GitHub repositories to check for
  to-be-reviewed PRs, like "opensource,private".
- `TBRBOT_APP_ID`: The GitHub App ID, found in https://github.com/organizations/*ORGNAME*/settings/apps/*APPNAME*
- `TBRBOT_APP_INSTALL`: The GitHub App Install ID for this installation, found in
  https://github.com/organizations/*ORGNAME*/settings/installations

Additionally, the bot supports the following environment variables which should ideally
be handled by secrets management infrastructure in cloud providers:
- `TBRBOT_APP_PRIVATE_KEY`: a PEM encoded private key, which can be generated in
  https://github.com/organizations/*ORGNAME*/settings/apps/*APPNAME*


### Reducing latency using GitHub webhooks
Normally the bot wakes up every hour to check for recently submitted PRs needing
followup. Its reaction to a submitted PR can be hastened by setting up a 
[GitHub webhook](https://docs.github.com/en/developers/webhooks-and-events/webhooks/about-webhooks)
for "Pull request reviews" events.

GitHub should be configured to deliver webhook events to `https://Public-DNS-name/webhook`

The bot expects to find the shared secret for validating webhook payloads in a `WEBHOOK_SECRET`
environment variable. The shared secret is configured in the webhook in
https://github.com/organizations/*ORGNAME*/settings/hooks/*WEBHOOK_ID*?tab=settings


### Monitoring
When used as part of the controls for Compliance requirements, it is important to 
to monitor whether the bot is working. Finding out on the eve of an audit that the
bot has been offline for an extended period would be ruinous.

In addition to `/webhook` the bot also exports metrics:
- `https://Tailscale-MagicDNS-name/debug/vars` in JSON format
- `https://Tailscale-MagicDNS-name/debug/varz` in Prometheus metric format

The `/debug` endpoints can only be reached from a local [Tailscale](https://tailscale.com)
tailnet. It is reasonable to allow public Internet access to https://Public-DNS-name/
for GitHub to be able to deliver webhooks, TBR-bot will restrict the other endpoints to
only be accessible via a private tailnet connection.

A metric of interest for monitoring is `tbrbot_repos_checked`, which counts the number of times
the bot has checked a repository for submitted PRs. This is expected to increment at least once
per hour.  An alert when `tbrbot_repos_checked` goes N hours with no change is a reasonable way
to monitor TBR-bot's operation.


### Hosting
The included Dockerfile and example fly.toml are suitable to run the tbr-bot
[hosted on fly.io](https://fly.io/).

We recommend forking this repository and making local modifications to the supplied fly.toml
to set it to the name of your instance and update the environment variables to correspond
to the GitHub repositories you want it to watch.

The bot needs a small amount of persistent storage for its Tailscale state, plus the various
configuration and secrets described above.
```
$ flyctl volumes create tbrbot_data --region sjc --size 1
$ flyctl scale count 1
$ flyctl secrets set TS_AUTHKEY=... TBRBOT_APP_ID=... TBRBOT_APP_INSTALL=...
$ flyctl secrets set TBRBOT_WEBHOOK_SECRET=...
$ flyctl secrets set TBRBOT_APP_PRIVATE_KEY=- < pem
$ flyctl ips allocate-v6
```

We recommend using a [one-time authkey with Tags set](https://tailscale.com/blog/acl-tags-ga/) to
authorize the bot to join the tailnet. Once the bot has run once and written its state
to persistent storage, the `TS_AUTHKEY` secret should be removed.
