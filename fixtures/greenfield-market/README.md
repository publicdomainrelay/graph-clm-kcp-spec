# greenfield-market

A market for compute that does not have any code yet. The two roles are
already named:

- **host**: runs the market, provisions a guest and keeps the ledger.
- **guest**: the machine the host provisions. It learns nothing about the
  network from the host; it reports its own address, node id or ticket out to
  the host once it has one.

The flow is declared before anything is written, so the architecture can be
reviewed while it is still a plan: the guest initiates the network report
toward the host over the relay, and the host never initiates a
network-discovery flow toward the guest.
