import { createServer, request as forward } from "node:http";

function port(name, fallback) {
  const value = Number(process.env[name] ?? fallback);
  if (!Number.isInteger(value) || value < 1024 || value > 65535) {
    throw new Error(`Invalid ${name}`);
  }
  return value;
}

const visitorAPort = port("SMOKE_SERVER_PORT", 3227);
const nextPort = port("SMOKE_NEXT_PORT", visitorAPort + 1);
const visitorBPort = port("SMOKE_VISITOR_B_PORT", visitorAPort + 2);
if (new Set([visitorAPort, nextPort, visitorBPort]).size !== 3) {
  throw new Error("The two visitor ports and Next port must be distinct");
}
const stripped = new Set([
  "host", "connection", "forwarded", "x-forwarded-for", "x-forwarded-host",
  "x-forwarded-proto", "x-real-ip", "x-vercel-forwarded-for",
]);
const servers = [];

try {
  for (const [listenPort, visitor] of [
    [visitorAPort, "198.51.100.206"],
    [visitorBPort, "2001:db8::206"],
  ]) {
    const server = createServer((incoming, outgoing) => {
      if (!incoming.url?.startsWith("/") || incoming.url.startsWith("//")) {
        outgoing.writeHead(400).end();
        return;
      }
      const headers = Object.fromEntries(Object.entries(incoming.headers).filter(
        ([name]) => !stripped.has(name) && !name.startsWith("x-agentmesh-share-"),
      ));
      // Model sanitized ingress; caller headers never choose either visitor.
      headers.host = `127.0.0.1:${listenPort}`;
      headers["x-forwarded-host"] = headers.host;
      headers["x-forwarded-proto"] = "http";
      headers["x-vercel-forwarded-for"] = visitor;
      const upstream = forward({
        hostname: "127.0.0.1", port: nextPort,
        path: incoming.url, method: incoming.method, headers,
      }, (response) => {
        outgoing.writeHead(response.statusCode ?? 502, response.headers);
        response.pipe(outgoing);
      });
      upstream.on("error", () => {
        if (!outgoing.headersSent) outgoing.writeHead(502);
        outgoing.end("Scratch frontend unavailable");
      });
      incoming.on("aborted", () => upstream.destroy());
      incoming.pipe(upstream);
    });
    servers.push(server);
    await new Promise((resolve, reject) => {
      server.once("error", reject);
      server.listen(listenPort, "127.0.0.1", resolve);
    });
  }
} catch (error) {
  for (const server of servers) server.close();
  throw error;
}

function stop() {
  for (const server of servers) {
    server.close();
    server.closeAllConnections();
  }
}
process.once("SIGTERM", stop);
process.once("SIGINT", stop);
