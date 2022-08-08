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


### Command line arguments
The bot expects to run continuously on a production system, and
supports the following command line arguments:
- `--org="example"`: The GitHub organization to use, like "tailscale".
- `--bugrepo="repo"`: The name of the repository to file followup issues in, like "private".
- `--appname="name"`: The name of the GitHub app account, like "tbr-bot", to use as the
  author in issues filed.
- `--repos="repo1,repo2"`: A comma-separated list of GitHub repositories to check for
  to-be-reviewed PRs, like "oss,private".
- `--appid="123"`: The GitHub App ID, found in https://github.com/organizations/*ORGNAME*/settings/apps/*APPNAME*
- `--appinstall="12345678"`: The GitHub App Install ID for this installation, found in
  https://github.com/organizations/*ORGNAME*/settings/installations
- `--keyfile="file.pem"`: The name of the file holding a PEM encoded private key,
  which can be generated in https://github.com/organizations/*ORGNAME*/settings/apps/*APPNAME*


### Reducing latency using GitHub webhooks
Normally the bot wakes up every hour to check for recently submitted PRs needing
followup. Its reaction to a submitted PR can be hastened by setting up a 
[GitHub webhook](https://docs.github.com/en/developers/webhooks-and-events/webhooks/about-webhooks)
for "Pull request reviews" events.

GitHub should be configured to deliver webhook events to `https://Public-DNS-name:10777/webhook`

- `--webhook_secret_file="filename.txt"`: The shared secret configured in the webhook in
  https://github.com/organizations/*ORGNAME*/settings/hooks/*WEBHOOK_ID*?tab=settings
- `--port="1234"` Listen on a port different from the default 10777.


### Monitoring
When used as part of the controls for Compliance requirements, it is important to 
to monitor whether the bot is working. Finding out on the eve of an audit that the
bot has been offline for an extended period would be ruinous.

In addition to `/webhook` the bot also exports metrics:
- `https://MagicDNS-name:10777/debug/vars` in JSON format
- `https://MagicDNS-name:10777/debug/varz` in Prometheus metric format

The `/debug` endpoints can only be reached from a local [Tailscale](https://tailscale.com)
tailnet. It is reasonable to allow public Internet access to https://servername:10777/
for GitHub to be able to deliver webhooks, TBR-bot will restrict the other endpoints to
only be accessible via a private tailnet connection.

A metric of interest for monitoring is `tbrbot_repos_checked`, which counts the number of times
the bot has checked a repository for submitted PRs. This is expected to increment at least once
per hour.  An alert when `tbrbot_repos_checked` goes N hours with no change is a reasonable way
to monitor TBR-bot's operation.
