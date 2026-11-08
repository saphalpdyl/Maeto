# frr-modules

FRR loadable modules created/used by for Maeto. Licensed **GPL-2.0-or-later**. The rest of the Maeto repository is Apache-2.0, so it had to be a separte root-level directory.

FRR itself is not here. It is the fork in `third_party/maeto-frr`. These
modules are built against it.

## Modules

| Module | Loaded into | Purpose |
|---|---|---|
| `bfdd_maeto.so` | `bfdd -M maeto` | Forwards BFD session state change notifications (`frr-bfdd` YANG) to the Maeto agent. |

## Building

FRR modules have no stable ABI, so they are compiled against a staged install
of the exact FRR build that will load them.

```sh
make stage   # stage-install third_party/maeto-frr/build into .frr-stage/
make         # build the modules
make compdb  # optional: compile_commands.json for CLion/clangd
```

Re-run `make stage` whenever the FRR fork changes.

Override `FRR_BUILD` if your FRR build directory is elsewhere.

## Loading during development

```sh
sudo bfdd -M "$PWD/bfdd_maeto.so" ...
```