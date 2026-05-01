/**
 * Raw TCP test for DataImpulse proxy credentials.
 * Run: npx tsx test-proxy.ts
 */
import "dotenv/config";
import net from "net";

const host = process.env.SCAN_PROXY_HOST!;
const port = Number(process.env.SCAN_PROXY_PORT ?? 823);
const user = process.env.SCAN_PROXY_USER!;
const pass = process.env.SCAN_PROXY_PASS!;

if (!host || !user || !pass) {
  console.error("Missing SCAN_PROXY_HOST / SCAN_PROXY_USER / SCAN_PROXY_PASS in .env");
  process.exit(1);
}

const auth = Buffer.from(`${user}:${pass}`).toString("base64");
console.log(`Connecting to ${host}:${port}`);
console.log(`Credentials: user="${user}" pass="${pass.slice(0, 3)}***" (${user.length}+${pass.length} chars)`);
console.log(`Base64: ${auth}`);
console.log();

// Try several CONNECT formats to find what DataImpulse accepts
const variants: [string, string][] = [
  ["with Host + Proxy-Connection",
   `CONNECT api.ipify.org:443 HTTP/1.1\r\nHost: api.ipify.org:443\r\nProxy-Authorization: Basic ${auth}\r\nProxy-Connection: Keep-Alive\r\n\r\n`],
  ["with User-Agent, no Proxy-Connection",
   `CONNECT api.ipify.org:443 HTTP/1.1\r\nHost: api.ipify.org:443\r\nProxy-Authorization: Basic ${auth}\r\nUser-Agent: curl/8.0.0\r\n\r\n`],
  ["minimal (no Host, no extra headers)",
   `CONNECT api.ipify.org:443 HTTP/1.1\r\nProxy-Authorization: Basic ${auth}\r\n\r\n`],
  ["HTTP/1.0",
   `CONNECT api.ipify.org:443 HTTP/1.0\r\nProxy-Authorization: Basic ${auth}\r\n\r\n`],
  ["NO AUTH (IP whitelist mode test)",
   `CONNECT api.ipify.org:443 HTTP/1.1\r\nHost: api.ipify.org:443\r\n\r\n`],
];

async function tryVariant(label: string, request: string): Promise<void> {
  return new Promise((resolve) => {
    const sock = net.createConnection(port, host, () => {
      console.log(`[${label}] TCP connected — sending CONNECT`);
      sock.write(request);
    });
    const timer = setTimeout(() => {
      console.log(`[${label}] timeout`);
      sock.destroy();
      resolve();
    }, 8_000);
    sock.once("data", (chunk) => {
      clearTimeout(timer);
      const resp = chunk.toString("utf8", 0, 200).replace(/\r\n/g, " | ");
      console.log(`[${label}] response: ${resp}`);
      sock.destroy();
      resolve();
    });
    sock.on("error", (e) => {
      clearTimeout(timer);
      console.log(`[${label}] error: ${e.message}`);
      resolve();
    });
  });
}

for (const [label, req] of variants) {
  await tryVariant(label, req);
}

console.log("\nDone. Any variant that says '200 Connection Established' is the one that works.");
