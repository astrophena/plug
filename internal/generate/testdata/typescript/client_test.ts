import { Client, OptionalError, Problem, TextError } from "./client.ts";

Deno.test("schema fields preserve HTTP error metadata", async () => {
  const details = {
    status: "pending",
    name: 7,
    code: true,
    details: ["reason"],
    stack: "c3RhY2s=",
    cause: "schema cause",
    constructor: "schema constructor",
    message: 42,
    toString: "schema string",
    valueOf: 9,
  };
  const client = new Client("https://example.test", {
    fetch: () =>
      Promise.resolve(Response.json({
        code: "problem",
        message: "transport message",
        details,
      }, { status: 400 })),
  });
  try {
    await client.fail();
    throw new Error("request succeeded");
  } catch (err) {
    if (!(err instanceof Problem)) throw err;
    const status: number = err.status;
    const schemaStatus: string | undefined = err.details?.status;
    const schemaMessage: number | undefined = err.details?.message;
    if (
      status !== 400 || err.code !== "problem" || err.name !== "Problem" ||
      schemaStatus !== "pending" || schemaMessage !== 42 ||
      err.details?.constructor !== "schema constructor" ||
      err.details?.details[0] !== "reason" ||
      err.details?.toString !== "schema string" || err.details?.valueOf !== 9 ||
      !err.toString().includes("transport message") ||
      !err.message.includes("transport message")
    ) {
      throw new Error("error metadata or schema fields changed");
    }
  }
});

Deno.test("string messages keep the generated error behavior", () => {
  const err = new TextError(400, "transport", {
    message: "schema message",
    value: "field",
  });
  if (err.message !== "schema message" || err.value !== "field") {
    throw new Error("string message or field changed");
  }
  const optional = new OptionalError(400, "transport", {});
  if (!optional.message.includes("transport")) {
    throw new Error("optional schema message replaced Error.message");
  }
});

Deno.test("schema text stays literal", async () => {
  let url = "";
  const client = new Client("https://example.test", {
    fetch: (input) => {
      url = String(input);
      return Promise.resolve(new Response(null, { status: 204 }));
    },
  });
  await client.get({ id: "a/b" });
  const want = "https://example.test/`+(globalThis.literalExecuted=true)+`/${text}/\\/a%2Fb";
  if (url !== want) throw new Error(`URL: ${url}; want ${want}`);
  if ("literalExecuted" in globalThis) throw new Error("path executed");
});

Deno.test("wildcard paths", async () => {
  for (
    const [path, want] of [
      [undefined, ""],
      ["", ""],
      ["docs/a file%?.txt", "docs/a%20file%25%3F.txt"],
      ["日本語/資料.txt", "%E6%97%A5%E6%9C%AC%E8%AA%9E/%E8%B3%87%E6%96%99.txt"],
    ]
  ) {
    const client = new Client("https://example.test", {
      fetch: (input) => {
        const url = new URL(String(input));
        const expected = "/repos/group%2Fnested%2Frepo/contents/" + want;
        if (
          url.pathname !== expected ||
          url.searchParams.get("rev") !== "feature/a" ||
          url.searchParams.has("path")
        ) {
          throw new Error(`unexpected URL: ${url}`);
        }
        return Promise.resolve(new Response(null, { status: 204 }));
      },
    });
    await client.browse({
      repository: "group/nested/repo",
      path,
      rev: "feature/a",
    });
  }
});
