export function unrestrictedDevOrigins(): string[] {
  // Next.js 16 has no disable switch for its development Origin check and
  // rejects a bare "**" pattern. This combination accepts normal FQDN/IPv4
  // origins plus opaque browser origins without tying dev access to one host.
  return ["**.*", "null"];
}
