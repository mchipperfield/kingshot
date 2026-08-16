# Changelog

All notable changes to this project are documented in this file.

## Unreleased

## [0.2.0] - 2026-08-16

### Added

- Added `/access view`, `/access set`, and `/access reset` commands for viewing and configuring access to bot management commands.
- Added persistent alliance access-role configuration in Firestore, including the user and timestamp responsible for each change.
- Added configurable permission checks for gift-code, bear-reminder, and access-management commands.
- Added support for granting command access through Discord's role hierarchy: members with the configured role or a higher-positioned role are permitted.
- Added fallback access through Manage Server and Administrator permissions when no custom access role is configured.
- Added recovery messaging when a configured Discord role has been deleted.

### Changed

- Bot command access now uses a configurable minimum role position rather than requiring separate role selections or a dedicated bot role.
- Access-role resets retain audit metadata while restoring the default Manage Server or Administrator requirement.
- Removed the administrator-only Discord command default from `/code`; runtime access checks now control its management subcommands.

## [0.1.0]

- Initial release.