// The transport the cloud-init module deploys: a guest-side subscriber that
// bridges the relay. The requester names it only through this helper.
export interface RelayTransport {
  proxyCommand(): string;
}

export function createRelayTransport(): RelayTransport {
  return {
    proxyCommand() {
      return "relay-subscriber --stdio %h %p";
    },
  };
}
