![ada](docs/images/maeto-logo-colorful.svg)
---

Maeto is a SRv6-based SD-WAN control plane. Computes directional least-cost paths across a PoP mesh and programs the kernel dataplane via FRR. It's a hobby project.

> [!NOTE]
> The repository on GitHub is a mirror maintained for visibility. Issue tracking and active development are done on [GitLab.](https://gitlab.com/saphalpdyl/Maeto)

[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)
[![Pipeline status](https://gitlab.com/saphalpdyl/maeto/badges/main/pipeline.svg)](https://gitlab.com/saphalpdyl/maeto/-/commits/main)
![GitLab Tag](https://img.shields.io/gitlab/v/tag/84541492?include_prereleases)

## Architecture
This is the conceptual architecture of the system excluding implementation features. v0 will use simple protobuf-based contracts instead of PCEP. The same goes for maeto agent shelling out for linux commands instead of Netlink.

![Conceptual Architecture of Maeto](/docs/images/arch/basic_arch.png)

---
<img src="docs/images/maeto-logo-monochrome.svg" width="70" height="30" style="vertical-align: middle;"> by saphalpdyl
