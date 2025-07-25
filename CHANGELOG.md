# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

<!--
Please add your changelog entry under this comment in the correct category (Security, Fixed, Added, Changed, Deprecated, Removed - in this order).

Changelog entries are best in the following format, where scope is something like "generic client" or "lbaas/v1"
(for LBaaS API bindings). If the change isn't user-facing but still relevant enough for a changelog entry, add
"(internal)" before the scope.

* (internal)? scope: short description (pull request, author)

Some examples, more below in the actual changelog (newer entries are more likely to be good entries):
* generic client: List resources with a channel (#42, @LittleFox94)
* core/v1: added helper methods to tag resources (#122, @marioreggiori)
* (internal) generic client: add hook FilterRequestURLHook (#123, @marioreggiori)

-->

### Added

* Reprovision failed volumes automatically (#305, @nachtjasmin)

## [0.1.9] -- 2025-07-17

* Increase storage limit to 10TiB (@nachtjasmin) (@nachtjasmin)

## [0.1.8] -- 2025-06-25

### Fixed

* Add proper error check for volume mount paths (#298, @nachtjasmin)

## [0.1.6] -- 2025-05-22

Smaller style changes.

### Added

* Add support for volume expansion. (#291, @nachtjasmin)

  StorageClasses now support the `allowVolumeExpansion: true` parameter, allowing for an *online*
  resizing of existing PersistentVolumeClaims.

  Right now, the csi-resize container is just added as a sidecar to the existing controller.
  We plan to extract this into a separate deployment in the near future.

## [0.1.5] -- 2025-05-14

### Fixed

* Use klog library to fix the logs introduced in #286. (#290, @nachtjasmin)

## [0.1.4] -- 2025-05-12

### Added

* Add detailed logs for the whole CSI workflow to assist in debugging. (#286, @nachtjasmin)

## [0.1.3] - 2025-02-14

### Fixed

Our valentine's gift to you! A little update of all the dependencies. (#264, @nachtjasmin)

## [0.1.2] - 2024-02-05

### Fixed

* Ensure that IP address is set when querying the storage server interface (#223, @nachtjasmin)

## [0.1.1] - 2024-03-14

### Fixed
* Fixed csi-driver image version in deploy manifests

## [0.1.0] - 2024-02-05

### Added
* Initial release
