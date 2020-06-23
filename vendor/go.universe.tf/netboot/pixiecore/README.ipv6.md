# PXE over IPv6 (WORK IN PROGRESS)

This is currently just a braindump of exploratory notes for supporting
PXE over IPv6, as supported by recent UEFI firmwares.

I'm using an Intel NUC to explore firmware behavior to fill in the
gaps not covered by specs. Claims made are made under the assumption
that other PXEv6 firmware behaves like the NUC's, which may not be
true. But it's all I have to go on right now.

Relevant RFCs:

 - [DHCPv6](https://tools.ietf.org/html/rfc3315)
 - [DHCPv6 Options for Network Boot](https://tools.ietf.org/html/rfc5970)
 - [DHCPv4 Options for Intel PXE](https://tools.ietf.org/html/rfc4578) (some enums carried over to DHCPv6)
 - [Preboot eXecution Environment](http://www.pix.net/software/pxeboot/archive/pxespec.pdf), our old friend the monster spec.

None of these specs describe the revised PXE booting process for
IPv6. All we know is that it's likely similar to the original PXE
process, and that we're using DHCPv6 rather than stateless
autoconfiguration messages (router solicitation, router advertisement,
etc.).

## Overall boot process

Typical DHCPv6 configuration process:
 - Client self-configures a link local address.
 - Client sends Router Solicitation to ff02::2 (all-routers), receives
   back Router Advertisements.
 - If told to do so by RAs, client does stateless autoconfiguration
   for a number of prefixes.
 - If told to do so by RAs, client sends DHCPv6 solicit to ff02::1:2,
   requesting Identity Associations (roughly, addresses and related
   config). Receives back DHCPv6 advertisements.
 - Client applies DHCPv6 algorithms for picking which IAs to commit
   to, sends DHCPv6 request(s) to finalize configuration. Receives
   DHCPv6 reply(s) back.

PXEv6 on NUC deviates from this. It will _always_ start DHCPv6
configuration after attempting stateless configuration. If it receives
Router Advertisements, it will apply any stateless autoconf indicated
by those RAs, but will _ignore_ the stateful configuration bits
(i.e. it will attempt DHCPv6 even if the RA says that no stateful
configuration is available). If it receives no RAs, it will happily
proceed to DHCPv6 regardless.

This is great! It means Pixiecore doesn't have to implement or care
about router soliciation/advertisement, because the firmware will
DHCPv6 regardless of what the RAs tell it about available network
services.

It also means we can make things Just Work even on completely
unconfigured networks (where only link-local addresses are available).

Within the DHCPv6 solicit, things are beautifully clean and free from
the historical baggage of DHCPv4. A client that requests option 59
(Boot File URL) is a PXE client, and if you send it that option, it
should do stuff. It's so simple I could cry.

The client architecture option from PXEv4 is carried over, so we can
differentiate various CPU architectures. In practice, I only expect to
see 0x7 (64-bit x86 EFI), given that PXEv6 support only exists in UEFI
so far, afaik.

## What addresses to use for DHCPv6?

The catch is that, contrary to IPv4, in an IPv6 world we should assume
that the majority of networks will _not_ have an incumbent DHCPv6
server, so providing ProxyDHCP responses only will not be sufficient
to achieve zero-ish config.

So, we'll have to hand out addresses, meaning Pixiecore will need to
be a full (albeit basic) DHCPv6 server. For addresses, we can use a
ULA prefix to both avoid collisions with globally routable prefixes,
and to avoid having to coordinate with other network services to
discover usable global prefixes. 

ULAs are not globally routable. We don't care, we're only configuring
it so the client can talk to Pixiecore, which is sitting on the same
L2 segment. If a Router Advertisement has provided the booting client
with a globally routable address as well, then great, that client can
also talk to the internet if it feels like it. If not, not our
problem.

ULAs are defined as being /48s within the fd00::/8 supernet. It is
recommended to generate a 40-bit random integer and use that to form
the ULA prefix. In our case we'll probably use 60 bits to end up with
a /64, since we don't need the full /48, and /64 is enough that we can
statelessly map client DUIDs to a full /128 address without having to
carry a state DB around with us.

In later versions, we might want to provide some kind of override to
let administrators tell Pixiecore what IP range to manage, but that's
a slippery slope that leads to building a fully featured DHCPv6
server. Ick.

Hrm. A complication is that we'd need to set a ULA within the right
prefix on the right interface for Pixiecore to have
connectivity. That's annoying. Maybe we can use link-local addresses
in the boot URL instead? But that has a good chance of not
working... We'll have to test.

## What happens after DHCPv6?

The Boot File URL provided by the DHCPv6 server tells the client where
to grab the file to chainload. Supported protocols are TFTP and, in
theory, HTTP. However, HTTP is a relatively recent addition to the
TianoCore UEFI reference implementation, so support is unlikely to be
universal. Sadly, the client provides no indication of what protocols
it supports.

So, for maximum compatibility, we're probably going to
stick to TFTP for the first stage bootloader. That's fine, we already
have TFTP support implemented and tested.

## After that?

The process should be identical to PXEv4: TFTP serves a copy of iPXE
suitable for the client architecture, and the ipxe has an embedded
script that drives the rest of the boot process: configure networking
again, grab the actual boot files with HTTP, and chainload into them.
