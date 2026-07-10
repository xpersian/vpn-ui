[English](/README.md) | [فارسی](/README_FA.md) | [العربية](/README_AR.md) | [中文](/README_ZH.md) | [Español](/README_ES.md) | [Русский](/README_RU.md) | [Türkçe](/README_TR.md)

<p align="center">
  <img src="https://raw.githubusercontent.com/Sir-MmD/vpn-ui/refs/heads/main/media/logo.png" alt="VPN-UI Logo" width="260">
</p>

Este proyecto es una versión mejorada del panel **[3X-UI](https://github.com/MHSanaei/3x-ui)** (versión 2.9.3). El objetivo de este proyecto es agregar diversos protocolos y ofrecerlo como un panel integral con soporte para las funciones de **Xray-core**.

## Nuevos protocolos

- PPTP
- L2TP (RAW)
- L2TP/IPsec
- OpenVPN

## Nuevas funcionalidades

- Función **Client to Client**, incluso como **Cross Inbound** (conexión interna de un usuario L2TP con un usuario OpenVPN)
- Incorporación de los **Encryption** **AES-256-GCM** y **AES-128-GCM** al protocolo **Shadowsocks**
- Soporte para **XHTTP Object** en el **Outbound**
- Script de instalación automática de **[WARP-CLI](https://github.com/Sir-MmD/warp-cli)** (la versión oficial de Cloudflare)
- Núcleo [**Xray-core** parcheado](https://github.com/Sir-MmD/Xray-core) para solucionar el error «Unsupported Cipher» en el protocolo **Shadowsocks**
- Empaquetado de todos los archivos (Geofile, Xray-core y los núcleos del Backend) dentro de un único archivo binario
- Exportación de los enlaces de las cuentas en formato **TXT** y **PDF**
- Incorporación de **checkbox** a los clientes y a los Inbound
- Función **Bulk Operation**: modificar de forma grupal el volumen de datos y el tiempo de los usuarios

## Sistemas operativos probados


| | Distribución |Versión |Versión |Versión |
|:---:|:---|:---:|:---:|:---:|
| <img src="https://cdn.simpleicons.org/ubuntu" width="32" height="32" alt="Ubuntu"> | **Ubuntu** | `22.04` | `24.04` | `26.04` |
| <img src="https://cdn.simpleicons.org/debian" width="32" height="32" alt="Debian"> | **Debian** | `12` | `13` | |
| <img src="https://cdn.simpleicons.org/fedora" width="32" height="32" alt="Fedora"> | **Fedora** | `43` | `44` | |
| <img src="https://cdn.simpleicons.org/almalinux/2F80ED" width="32" height="32" alt="AlmaLinux"> | **AlmaLinux** | `8` | `9` | `10` |
| <img src="https://cdn.simpleicons.org/rockylinux" width="32" height="32" alt="Rocky Linux"> | **Rocky Linux** | `8` | `9` | `10` |
| <img src="https://cdn.simpleicons.org/archlinux" width="32" height="32" alt="Arch Linux"> | **Arch Linux** | `Rolling` | | |


> [!IMPORTANT]
> Se recomienda instalar el panel siempre en los sistemas operativos probados, ya que es muy probable que los nuevos núcleos no funcionen correctamente en los demás sistemas operativos.

## Instalación del panel

```bash
curl -Ls https://raw.githubusercontent.com/Sir-MmD/vpn-ui/refs/heads/main/deploy.sh | sudo bash
```

## Desinstalación del panel

```bash
sudo /opt/vpn-ui/vpn-ui-amd64 --uninstall
```

> [!NOTE]
> La ruta de la base de datos, el servicio **systemd** y todos los puertos predeterminados han cambiado, así que puedes instalar este panel junto a tus otros paneles sin ningún problema.

## Capturas de pantalla

![Vista general](https://raw.githubusercontent.com/Sir-MmD/vpn-ui/refs/heads/main/media/overview.png)
![Configuración del núcleo](https://raw.githubusercontent.com/Sir-MmD/vpn-ui/refs/heads/main/media/core_Settings.png)


## Cómo interactúan los nuevos protocolos con el núcleo de Xray-core

```mermaid
flowchart TB
  Client["VPN Client<br/>(L2TP/IPsec · PPTP · OpenVPN)"]

  subgraph PANEL["vpn-ui panel — root process"]
    PROC["procmgr<br/>supervises the daemons"]
    RAD["in-binary RADIUS<br/>127.0.0.1:1812 auth · :1813 acct"]
    HOOK["OpenVPN hooks<br/>auth / connect / disconnect / evict"]
    CONF["writes Xray config:<br/>dokodemo-door inbound +<br/>per-account source-IP routing"]
    STAT["reads Xray stats (gRPC)<br/>enforces traffic / device limits"]
  end

  subgraph DAEMON["Bundled VPN daemons (panel children)"]
    D["xl2tpd + libreswan · pptpd · openvpn<br/>(pppd for L2TP/PPTP)"]
  end

  subgraph KERNEL["Linux kernel data plane"]
    IFACE["ppp0 / tun0<br/>client is assigned a pool IP"]
    NFT["nftables mark:<br/>UDP → TPROXY · TCP → REDIRECT"]
    RULE["ip rule fwmark 1 → table 100"]
  end

  subgraph XRAY["Xray-core (bundled, panel-managed)"]
    DOKO["dokodemo-door inbound<br/>sockopt tproxy, mark 255"]
    ROUTE{"routing:<br/>match source IP → account"}
    OUT["outbound<br/>freedom / proxy / WARP"]
  end

  NET["Internet"]

  %% control plane
  Client -->|"tunnel + credentials"| D
  D -.->|"MS-CHAPv2 Access-Request"| RAD
  RAD -.->|"Accept + pool IP"| D
  D -.->|"user-pass / client-connect"| HOOK
  HOOK -.->|"lease per-account IP"| D
  PROC --- D
  CONF --> DOKO
  CONF --> ROUTE

  %% data plane
  D -->|"decapsulated packets"| IFACE
  IFACE --> NFT --> RULE --> DOKO
  DOKO --> ROUTE --> OUT --> NET

  %% accounting + return
  OUT -.->|"per-account counters"| STAT
  STAT -.->|"disconnect over-limit"| RAD
  NET -.->|"replies (symmetric path back)"| OUT
```

## Compilación desde el código fuente

```bash
git clone https://github.com/Sir-MmD/vpn-ui.git && cd vpn-ui
./build.sh
```

## Prueba E2E

![Prueba E2E](https://raw.githubusercontent.com/Sir-MmD/vpn-ui/refs/heads/main/media/test_unit.png)

Se ha diseñado para este proyecto una prueba **E2E** completa en Python dentro de la carpeta `test_unit`, que puedes utilizar. Los pasos son los siguientes:

1. Entra en la carpeta `test_unit` e introduce la configuración que desees en `config.toml`.
2. Ejecuta el script `setup.sh`.
3. Coloca el archivo binario compilado dentro de la carpeta `test_subject`.
4. Ejecuta `run.sh` con permisos de `sudo`.

> [!IMPORTANT]
> La prueba E2E completa consume muchísimo tiempo; si solo hiciste un cambio pequeño en el proyecto, es mejor que pruebes únicamente esa parte con el switch `--tests`:

| Test ID | Description |
| :--- | :--- |
| `core-init` | provision kernel modules + packages + xray core |
| `server-setup` | create inbounds + accounts + source-IP routing rules |
| `openvpn` | connect variants + checks + peer reachability (OpenVPN) |
| `l2tp` | connect variants + checks + peer reachability (L2TP/IPsec) |
| `pptp` | connect variants + checks + peer reachability (PPTP) |
| `bulk-ops` | bulk client add/sub/enable/disable + TXT/PDF export via API |
| `backup-restore` | DB export + import round-trip |
| `warp-socks` | Cloudflare warp-cli SOCKS install + egress |
| `random-cfg` | `--random` switch: randomize port + creds + webpath, then restore |
| `systemd` | `--systemd` switch: install + run the panel as a systemd unit |
| `uninstall` | `--uninstall` switch: install everything, tear down, assert clean host |
| `export-js` | host-side Node TXT/PDF export test (no VM) |

Para probar solo en un sistema operativo específico, también puedes usar el switch `--only`:

```bash
sudo ./run.sh --only ubuntu-24
```

## Donaciones

🔹USDC-Polygon: ```0xdC2Ab962954e8fA1502C44656c5A32CF2979568C```

🔹USDT-BEP20: ```0xdC2Ab962954e8fA1502C44656c5A32CF2979568C```

🔹USDT-TRC20: ```TXEhckDXtdLGAjP5PZXfNnQjPHzEVTcBmR```

🔹TRX: ```TXEhckDXtdLGAjP5PZXfNnQjPHzEVTcBmR```

🔹LTC: ```ltc1qmapmnuf6cq9x679nmu0k4uyq779mxxcwnkgdll```

🔹BTC: ```bc1q62w7lyndzndsp74vj4dsayvun8xnapzq6hx5ea```

🔹ETH: ```0xdC2Ab962954e8fA1502C44656c5A32CF2979568C```
