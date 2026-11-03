import { expect, test } from "@playwright/test";
import { createServer, type ServerResponse } from "node:http";

test("server queue states, cancellation and the next reply stay isolated", async ({
  page,
}) => {
  const responses: ServerResponse[] = [];
  const server = createServer(async (request, response) => {
    if (!request.url?.startsWith("/public/v1/")) {
      const source = await fetch("http://127.0.0.1:18080" + request.url);
      source.headers.forEach((value, name) => {
        if (!["content-length", "content-encoding"].includes(name))
          response.setHeader(name, value);
      });
      response.writeHead(source.status);
      response.end(new Uint8Array(await source.arrayBuffer()));
      return;
    }
    response.setHeader(
      "Access-Control-Allow-Origin",
      request.headers.origin ?? "http://127.0.0.1:18080",
    );
    response.setHeader("Access-Control-Allow-Credentials", "true");
    response.setHeader("Access-Control-Allow-Methods", "GET, POST, OPTIONS");
    response.setHeader(
      "Access-Control-Allow-Headers",
      "Content-Type, X-Provider-API-Key, X-Prompt-Shield-Progress",
    );
    request.resume();
    if (request.method === "OPTIONS") {
      response.writeHead(204);
      response.end();
      return;
    }
    if (request.url?.endsWith("/models")) {
      response.setHeader("Content-Type", "application/json");
      response.end(
        JSON.stringify({ data: [{ id: "fixture", name: "Fixture" }] }),
      );
      return;
    }
    expect(request.headers["x-prompt-shield-progress"]).toBe("v1");
    response.writeHead(200, { "Content-Type": "text/event-stream" });
    response.write('event: queued\ndata: {"position":2}\n\n');
    responses.push(response);
  });
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  const address = server.address();
  if (!address || typeof address === "string")
    throw Error("No fixture address");
  try {
    await page.route("**/config.json", (route) =>
      route.fulfill({
        json: {
          apiBaseURL: "",
          productMode: "community",
        },
      }),
    );
    await page.goto(`http://127.0.0.1:${address.port}/en`);
    await page
      .getByLabel("Provider API key", { exact: true })
      .fill("synthetic-queue-key");
    await page
      .getByRole("button", { name: "Use key for this session", exact: true })
      .click();
    await page.getByLabel("Model", { exact: true }).selectOption("fixture");
    await page.getByLabel("Your prompt", { exact: true }).fill("Queue fixture");
    const send = page.getByRole("button", {
      name: "Send protected message",
      exact: true,
    });
    const progress = page.getByRole("status").filter({
      hasText:
        /^(Sending your request|You are in the queue|Your request is being processed)/,
    });
    await send.click();
    await expect(progress).toHaveText("You are in the queue — position: 2");
    await page.getByRole("button", { name: "Cancel", exact: true }).click();
    await expect(send).toBeEnabled();
    await expect.poll(() => responses[0]?.destroyed).toBe(true);
    responses[0].end(
      'event: processing\ndata: {}\n\nevent: result\ndata: {"choices":[{"message":{"content":"Obsolete reply"}}]}\n\n',
    );
    await send.click();
    await expect(progress).toHaveText("You are in the queue — position: 2");
    expect(responses).toHaveLength(2);
    responses[1].write("event: processing\ndata: {}\n\n");
    await expect(progress).toHaveText("Your request is being processed…");
    responses[1].end(
      'event: result\ndata: {"choices":[{"message":{"content":"Fresh reply"}}]}\n\n',
    );
    await expect(page.getByText("Fresh reply", { exact: true })).toBeVisible();
    await expect(page.getByText("Obsolete reply", { exact: true })).toHaveCount(
      0,
    );
    await expect(progress).toHaveCount(0);
  } finally {
    for (const response of responses) response.destroy();
    server.closeAllConnections();
    await new Promise<void>((resolve) => server.close(() => resolve()));
  }
});
