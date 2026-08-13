# Privacy Policy

**Effective date:** 13 August 2026

This privacy policy explains how the KingShot Discord service handles information when you use its Discord bot and related services.

## Information We Process

When you use the service, we may process:

- Your Discord user ID and username, to identify your Discord account and associate your requests with your account.
- KingShot player IDs and kingdom IDs that you submit.
- Discord guild, channel, and related configuration IDs needed to operate the service and post redemption reports.
- Gift codes that you submit to the service.
- Service and operational logs needed to maintain reliability and security.

We do not need your Discord password. Discord handles authentication through its OAuth service, and the service receives the identity information permitted by the `identify` scope.

## How We Use Information

We use this information to:

- Link registered KingShot players to the Discord account that registered them.
- Redeem gift codes for registered players.
- Maintain service state and prevent duplicate gift-code processing.
- Post relevant redemption results to configured Discord guild channels.
- Authenticate requests to manage or remove your data.
- Operate, secure, troubleshoot, and improve the service.

We do not sell your personal information.

## Third-Party Services

The service relies on third-party services, including:

- Discord, for account authentication and Discord interactions.
- Google Cloud Firestore, for persistent service data.
- The KingShot gift-code API, when redeeming gift codes.
- Google Cloud App Engine, for hosting the privacy service.

These services may process information according to their own privacy policies and terms.

## Data Retention

We retain information only for as long as it is needed to provide and secure the service. Retention may also be affected by backups, service logs, and the retention policies of the third-party services listed above.

## Deleting Your Data

You can request deletion of the data associated with your Discord account by visiting:

[Request deletion of my data](https://kingshot-8539b.ew.r.appspot.com/delete)

The deletion flow authenticates you with Discord before processing the request. When completed, the service deletes player documents associated with your Discord user ID and removes your user ID from applicable alliance configuration data. Deletion does not necessarily remove information held by Discord, the KingShot game, third-party providers, backups, or logs that are retained for security or operational reasons.

If the deletion flow cannot complete, it will report an error and the request may need to be submitted again.

## Security

We use reasonable technical measures to protect the information processed by the service. No internet service can guarantee absolute security. Do not share private credentials, OAuth tokens, or session cookies with anyone.

## Changes To This Policy

We may update this policy when the service or its data practices change. The effective date at the top of this document indicates when the current version took effect.

## Contact

For questions about this policy or a privacy request, contact the operator or maintainers of the KingShot service through the service's official support channel.
