import { assertEquals } from "jsr:@std/assert@^1.0.19";

import { Greeter, greet, shout } from "./mod.ts";

Deno.test("greet keeps the name and writes the text", () => {
  assertEquals(greet("world"), { name: "world", text: "hello, world" });
});

Deno.test("shout uppercases the greeting", () => {
  assertEquals(shout("world"), "HELLO, WORLD");
});

Deno.test("Greeter prefixes the greeting", () => {
  assertEquals(new Greeter("hi").greeting("world"), "hi world");
});
