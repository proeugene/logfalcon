Only public Ed25519 keys (lowercase hex, 32 bytes, named KEY_ID.pub) belong here.
Production private keys and seeds must never enter this repository.

Release signing deliberately fails until a maintainer provisions at least one
public key and the matching CI secret/variable. See docs/OFFLINE_UPDATES.md.
The optional rotation.json is an array of public, signed delegation certificates;
retain the cumulative chain for appliances that skip intermediate releases.
