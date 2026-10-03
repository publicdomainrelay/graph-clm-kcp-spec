export interface Greeting {
  name: string;
  text: string;
}

export function greet(name: string): Greeting {
  return { name, text: `hello, ${name}` };
}

export function shout(name: string): string {
  return greet(name).text.toUpperCase();
}

export class Greeter {
  constructor(private readonly prefix: string) {}

  greeting(name: string): string {
    return `${this.prefix} ${name}`;
  }
}
