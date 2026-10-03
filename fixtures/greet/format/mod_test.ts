import { assertEquals } from "jsr:@std/assert@^1.0.19";

import { titleCase, trimAll } from "./mod.ts";

Deno.test("titleCase capitalises every word", () => {
  assertEquals(titleCase("hello world"), "Hello World");
});

Deno.test("trimAll trims every value", () => {
  assertEquals(trimAll([" a ", "b "]), ["a", "b"]);
});
