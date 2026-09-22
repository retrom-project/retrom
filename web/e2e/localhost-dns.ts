import dns from "node:dns";

// Chrome resolves .localhost to loopback itself; Node delegates to system DNS.
// Keep request URLs and Host headers intact so origin isolation is still tested.
export function installLocalhostDNS(): () => void {
  const lookup = dns.lookup;
  dns.lookup = ((hostname: string, ...options: unknown[]) => Reflect.apply(lookup, dns, [
    hostname.toLowerCase().endsWith(".localhost") ? "127.0.0.1" : hostname,
    ...options,
  ])) as typeof dns.lookup;
  return () => {dns.lookup = lookup;};
}
