// Cloud-init user_data composition. A UserDataModule renders one fragment and
// the registry collects them into a single #cloud-config. The guest-side
// transport is installed here, by a module, never by hand.

import type { CloudInitContext } from "@market-mini/market-common";
import { relayServiceName } from "@market-mini/market-common";

export interface UserDataModule {
  readonly name: string;
  render(ctx: CloudInitContext): string;
}

export class UserDataRegistry {
  private readonly modules: UserDataModule[] = [];

  register(module: UserDataModule): void {
    this.modules.push(module);
  }

  render(ctx: CloudInitContext): string {
    const sections = this.modules.map((module) => module.render(ctx));
    return ["#cloud-config", ...sections].join("\n");
  }
}

// The transport the RFP cloud-init deploys. sshd stays on loopback and a
// guest-side subscriber bridges the relay websocket to it, so the requester
// reaches the guest with ssh ProxyCommand and never dials a guest address.
export interface RelayTransport {
  readonly module: UserDataModule;
  proxyCommand(): string;
}

export function createRelayTransport(): RelayTransport {
  const module: UserDataModule = {
    name: "relay-transport",
    render(ctx) {
      return [
        "write_files:",
        `  - path: /etc/relay-transport.env`,
        `    content: SERVICE=${relayServiceName(ctx.vmName)}`,
        "runcmd:",
        "  - systemctl enable --now relay-subscriber.service",
      ].join("\n");
    },
  };
  return {
    module,
    proxyCommand() {
      return "relay-subscriber --stdio %h %p";
    },
  };
}

export function createAuthorizedKeysModule(key: string): UserDataModule {
  return {
    name: "authorized-keys",
    render() {
      return `ssh_authorized_keys:\n  - ${key}`;
    },
  };
}

export function createDefaultRegistry(): UserDataRegistry {
  const registry = new UserDataRegistry();
  registry.register(createRelayTransport().module);
  registry.register(createAuthorizedKeysModule("ssh-ed25519 AAAA fixture"));
  return registry;
}
