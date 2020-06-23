# Netboot, packages and utilities for network booting

This repository contains Go implementations of network protocols used
in booting machines over the network, as well as utilites built on top
of these libraries.

**This project is no longer actively developed. I'm glad if you find it useful, but don't expect any significant changes.**

## Programs

- [Pixiecore](https://github.com/danderson/netboot/tree/master/pixiecore): Command line all-in-one tool for easy netbooting

## Libraries

The canonical import path for Go packages in this repository is `go.universe.tf/netboot`.

- [pcap](https://godoc.org/go.universe.tf/netboot/pcap): Pure Go implementation of reading and writing pcap files.
- [dhcp4](https://godoc.org/go.universe.tf/netboot/dhcp4): DHCPv4 library providing the low-level bits of a DHCP client/server (packet marshaling, RFC-compliant packet transmission semantics).
- [tftp](https://godoc.org/go.universe.tf/netboot/tftp): Read-only TFTP server implementation.
- [pixiecore](https://godoc.org/go.universe.tf/netboot/pixiecore): Go library for Pixiecore tool functionality. Every stability warning in this repository applies double for this package.

