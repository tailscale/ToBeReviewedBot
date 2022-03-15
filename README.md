This bot is expected to be run periodically as a GitHub Action
using the identity of a GitHub App. It will check recently submitted
Pull Requests for any which went in ToBeReviewed (i.e. without
any Approval on the PR).

It will file an issue to followup on the review.

To run, the bot expects to find the following environment variables:
- TBRBOT\_ORG: The GitHub organization to use, like "tailscale"
- TBRBOT\_BUGREPO: The name of the repository to file followup issues in, like "private".
- TBRBOT\_APPNAME: The name of the GitHub app account, like "tbr-bot", to use as the author in issues filed.
- TBRBOT\_REPOLIST: A comma-separated list of GitHub repositories to check for to-be-reviewed PRs, like "oss,private"
- GH\_APP\_ID: the GitHub App ID, found in https://github.com/organizations/*ORGNAME*/settings/apps/*APPNAME*
- GH\_APP\_INSTALL\_ID: the GitHub App Install ID for this installation, found in https://github.com/organizations/*ORGNAME*/settings/installations
- GH\_APP\_PRIVATE\_KEY: the private key, can be generated in https://github.com/organizations/*ORGNAME*/settings/apps/*APPNAME*

By convention all of them are set as Actions Secrets in the repository running the bot.
