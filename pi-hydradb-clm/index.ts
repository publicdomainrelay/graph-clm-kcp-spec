import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";
import { hydraClmExtension, optionsFromEnv } from "./src/extension.ts";

export default function hydradbClm(pi: ExtensionAPI): void {
  hydraClmExtension(pi, optionsFromEnv());
}
