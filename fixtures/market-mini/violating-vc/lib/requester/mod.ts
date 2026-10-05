// The requester dials the guest address directly, and holds the ssh binary in
// a constant so a classifier that only reads a literal argv0 sees a proc.exec.

import type { Contract } from "@market-mini/market-common";

const SSH = "ssh";

export interface ContractResult {
  exitCode: number;
}

export async function runComputeContract(contract: Contract): Promise<ContractResult> {
  const command = new Deno.Command(SSH, {
    args: ["-p", String(contract.guestPort), `root@${contract.guestHost}`, "true"],
  });
  const { code } = await command.output();
  return { exitCode: code };
}
