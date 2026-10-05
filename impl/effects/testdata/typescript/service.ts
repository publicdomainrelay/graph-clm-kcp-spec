import { handler } from "./handler.ts";

const DECOY = "Deno.Command('ssh') is only mentioned here";

const URL_DECOY = "https://decoy.example/health";

// fetch("https://decoy.example/comment")

export function registerRoutes(app) {
  app.get("/health", (c) => c.json({ ok: true }));
  app.post("/xrpc/com.example.market.evaluate", handler);
  app.route("/", sub);
}

export async function report(url: string) {
  const response = await fetch(url);
  await agent.callService(url, "com.example.record", "com.example.record", {});
  await agent.createRepoRecord("com.example.record", { value: 1 });
  await agent.createSignedRepoRecord("com.example.record", { value: 1 });
  const existing = await agent.getRecord(did, "com.example.record", rkey);
  return { response, existing };
}

export function spawnSessions() {
  const ssh = new Deno.Command("ssh", {
    args: [
      "-o",
      "ProxyCommand=websocat --binary wss://relay.example/tunnel",
      "-o",
      "BatchMode=yes",
      "root@guest.internal",
      "true",
    ],
  });
  const docker = new Deno.Command("docker", { args: ["exec", "-i", "guest", "sh"] });
  const keygen = new Deno.Command("ssh-keygen", { args: ["-t", "ed25519", "-N", ""] });
  return { ssh, docker, keygen };
}

export function listen() {
  Deno.serve({ port: 8080, hostname: "127.0.0.1" }, (request) => new Response("ok"));
  const listener = Deno.listen({ port: 9999 });
  const direct = Deno.connect({ hostname: "10.0.0.5", port: 22 });
  const socket = new WebSocket("wss://relay.example/tunnel");
  return { listener, direct, socket };
}
