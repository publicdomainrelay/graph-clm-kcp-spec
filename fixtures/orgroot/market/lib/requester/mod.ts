// The requester posts a compute request and then reaches the provisioned guest.
// It never dials a guest address: ssh goes through the relay with ProxyCommand.

export interface ComputeRequest {
  readonly vmName: string;
  readonly image: string;
}

export function postRequest(request: ComputeRequest): Promise<string> {
  return Promise.resolve(`rfp:${request.vmName}`);
}

export function sshThroughRelay(vmName: string, relayUrl: string): string[] {
  return [
    "ssh",
    "-o",
    `ProxyCommand=websocat --binary ${relayUrl}/guest/${vmName}`,
    `root@${vmName}`,
  ];
}
