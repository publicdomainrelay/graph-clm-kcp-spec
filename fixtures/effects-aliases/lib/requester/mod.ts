// The requester reaches the guest through the relay. The argv0 and the host
// are held in names, the way a real module factors its command line.

import { createRelayTransport } from "@effects-aliases/relay";

export const SSH = "ssh";
export const GUEST_HOST = "10.0.0.7";

export async function runComputeContract(guestPort: number): Promise<number> {
  const transport = createRelayTransport();
  const command = new Deno.Command(SSH, {
    args: ["-p", String(guestPort), `-o`, `ProxyCommand=${transport.proxyCommand()}`, `root@${GUEST_HOST}`, "true"],
  });
  const { code } = await command.output();
  return code;
}
