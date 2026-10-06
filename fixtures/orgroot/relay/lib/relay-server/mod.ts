// The relay is the registry. A guest subscribes under its name; a requester's
// ProxyCommand connects to the same name and the relay joins the two sockets.

const subscribers = new Map<string, WebSocket>();

export function handle(request: Request): Response {
  const { pathname } = new URL(request.url);
  const name = pathname.split("/").pop() ?? "";
  const { socket, response } = Deno.upgradeWebSocket(request);
  subscribers.set(name, socket);
  return response;
}

export function registered(name: string): boolean {
  return subscribers.has(name);
}
