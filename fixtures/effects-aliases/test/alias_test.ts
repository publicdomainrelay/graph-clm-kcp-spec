// The integration test drives the requester and dials the guest address held
// in a name, the way a test that builds its contract from a helper does.

import { runComputeContract, GUEST_HOST } from "@effects-aliases/requester";

export const GUEST_PORT = 2222;

Deno.test("the contract runs", async () => {
  const connection = await Deno.connect({ hostname: GUEST_HOST, port: GUEST_PORT });
  connection.close();
  const code = await runComputeContract(GUEST_PORT);
  if (typeof code !== "number") {
    throw new Error("no exit code");
  }
});
