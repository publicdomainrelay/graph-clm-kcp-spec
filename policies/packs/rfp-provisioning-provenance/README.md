# rfp-provisioning-provenance

The opt-in half of the RFP flow's guest invariants: a guest's transport and its
ssh key material come from the cloud-init `user_data` the bidder applies, so
neither may be hand-assembled in another file. Two templates over the
`CodeDiff` and the `ArchitectureModel`; the catalogue (`CATALOGUE.md`) is
generated from them.

These two templates used to live in `rfp-guest-isolation`. They moved out
because they guard *provisioning* (they need a CodeDiff and an executing or
writing effect at the added line), while `rfp-guest-isolation` guards the
running guest. The library's ported `provisioning-new-guest-transport` and
`provisioning-manual-key-material` rules are opt-in in the same way:
`policies/library` is never seeded into an example run by default, and neither
is this pack.

Bind it with an import in a repository's `policies.yaml`:

```yaml
imports:
  - pack: rfp-provisioning-provenance
    source: embedded
    version: v1
```

The importing binding must declare a `guest` role: the rule reads
`specd.file_in_role`, so the guest's files (the `user_data` module) are the
allowed place.

## Known limits

- **The pack carries its own copy of `lib/specd.rego`.** A pack is a
  self-contained tree, so the shared helper library is duplicated here and in
  `rfp-guest-isolation`; `specctl policy build` refreshes both from the same
  source and `TestExampleDistAndCatalogueAreCurrent` fails when they drift.
