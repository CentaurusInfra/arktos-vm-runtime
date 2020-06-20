# Testing pixiecore

This is a braindump of the different hardware configurations that
exhibit different behaviors.

| Machine | Firmware | PXE firmware | Notes |
| --- | --- | --- | --- |
| Proxmox VM | SeaBIOS | iPXE | |
| Proxmox VM | OVMF | OVMF | Good UEFI baseline, many UEFI firmwares are derived from OVMF/TianoCore/EDK2e |
| VirtualBox (OSS) | VirtualBox BIOS | customized iPXE | Cannot boot with `next-server` and `filename`, must be booted with iPXE-specific commands |
| VirtualBox (w/ extension pack) | VirtualBox BIOS | Intel UNDI | Good BIOS baseline, Intel UNDI is the basis for many BIOS PXE firmwares |
| VirtualBox (OSS) | VirtualBox EFI | ?? | Unclear what the EFI firmware is, but provides more diversity |
| Dell R610 | Dell BIOS | Dell | The only bare metal machine I have right now with remote management. Strange custom firmwares, also a decent variety test. |
