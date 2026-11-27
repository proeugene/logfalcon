Only public Ed25519 keys (lowercase hex, 32 bytes, named KEY_ID.pub) belong here.
Production private keys and seeds must never enter this repository.

Images built with field-2026.pub trust field updates signed on the maintainer's
Mac. A pilot can upload a newer matching-ABI .lfu over Wi-Fi on first boot.
The source and release installers also provision the keys in this directory.

Release signing requires a committed public key and the matching GitHub Actions
secret and key ID. See the [signing instructions](../docs/OFFLINE_UPDATES.md#maintainer-signing-setup).
The optional rotation.json is an array of public, signed delegation certificates;
retain the cumulative chain for appliances that skip intermediate releases.
