package service

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/mhsanaei/3x-ui/v2/awgsrc"
)

// awgBuildDir is where the embedded module source is extracted for the DKMS build.
const awgBuildDir = "/usr/src/vpn-ui-amneziawg"

// ensureAmneziawg builds + loads the out-of-tree `amneziawg` kernel module from the vendored
// source via DKMS, installing the toolchain + kernel headers first. This is the ONLY on-host
// compile in the project (a kernel module must match the running kernel, so it can't be
// prebuilt like the static daemons). On any failure it returns a Warn step (AmneziaWG
// unavailable, every other protocol keeps working) with the build log — there is no userspace
// fallback by design. DKMS registration makes the module auto-rebuild on kernel upgrade.
func ensureAmneziawg() ProvisionStep {
	name := "AmneziaWG (amneziawg kernel module)"
	if moduleAvailable(amneziawgModule) {
		return ProvisionStep{Name: name, OK: true, Msg: "amneziawg module already available"}
	}

	pm := detectPackageManager()
	if pm == nil {
		return ProvisionStep{Name: name, OK: true, Warn: true,
			Msg: "no supported package manager — install dkms + kernel headers + a C toolchain, then re-run setup"}
	}

	var log strings.Builder
	warn := func(msg string) ProvisionStep {
		return ProvisionStep{Name: name + " via " + pm.name, OK: true, Warn: true, Msg: msg, Log: log.String()}
	}

	// EPEL first on EL (dkms lives in EPEL there). Best-effort.
	if isEnterpriseLinux() {
		out, err := pm.installPackage("epel-release")
		log.WriteString(out)
		if err != nil {
			log.WriteString("\n(epel-release install failed; dkms may be unavailable: " + err.Error() + ")\n")
		}
	}

	// Build prerequisites: dkms, a C toolchain (gcc+make), and headers for the running kernel.
	for _, pkg := range awgBuildDeps() {
		if pkg == "" {
			continue
		}
		out, err := pm.installPackage(pkg)
		log.WriteString(out)
		if err != nil {
			// A header package pinned to the exact running kernel can be gone (box behind the
			// archive); try the generic fallback before giving up.
			installed := false
			for _, fb := range awgHeaderFallbacks(pkg) {
				fout, ferr := pm.installPackage(fb)
				log.WriteString(fout)
				if ferr == nil {
					installed = true
					break
				}
			}
			if !installed {
				return warn("failed to install build prerequisite '" + pkg + "' — AmneziaWG unavailable: " + err.Error())
			}
		}
	}

	// Extract the vendored source and DKMS-build it (mirrors the proven manual flow:
	// make dkms-install -> dkms add/build/install -> modprobe).
	_ = os.RemoveAll(awgBuildDir)
	if err := awgsrc.Extract(awgBuildDir); err != nil {
		return warn("failed to extract bundled amneziawg source: " + err.Error())
	}
	steps := [][]string{
		{"make", "-C", awgBuildDir, "dkms-install"},
		{"dkms", "add", "-m", amneziawgModule, "-v", awgsrc.Version},
		{"dkms", "build", "-m", amneziawgModule, "-v", awgsrc.Version},
		{"dkms", "install", "-m", amneziawgModule, "-v", awgsrc.Version},
	}
	for _, st := range steps {
		out, err := awgRunCmd(st[0], st[1:]...)
		log.WriteString(fmt.Sprintf("\n$ %s\n%s", strings.Join(st, " "), out))
		if err != nil {
			// `dkms add` errors if the module is already registered — not fatal.
			if st[1] == "add" && strings.Contains(strings.ToLower(out), "already") {
				continue
			}
			return warn("DKMS build failed at '" + strings.Join(st, " ") + "' — AmneziaWG unavailable: " + err.Error())
		}
	}

	modprobeOut, _ := awgRunCmd("modprobe", amneziawgModule)
	log.WriteString("\n$ modprobe amneziawg\n" + modprobeOut)

	if !moduleAvailable(amneziawgModule) {
		return warn("amneziawg built but not loadable (check Secure Boot / dmesg) — AmneziaWG unavailable")
	}

	// Persist so the module autoloads at boot.
	_ = os.WriteFile("/etc/modules-load.d/amneziawg.conf", []byte("amneziawg\n"), 0644)
	return ProvisionStep{Name: name + " via " + pm.name, OK: true,
		Msg: "amneziawg module built + loaded (DKMS " + awgsrc.Version + ")", Log: log.String()}
}

// isEnterpriseLinux reports whether the host is an EL rebuild (Alma/Rocky/CentOS/RHEL), where
// dkms comes from EPEL.
func isEnterpriseLinux() bool {
	switch strings.ToLower(osReleaseField("ID")) {
	case "almalinux", "rocky", "centos", "rhel":
		return true
	}
	return strings.Contains(strings.ToLower(osReleaseField("ID_LIKE")), "rhel")
}

// awgBuildDeps returns the packages to install before the DKMS build (dkms, a C toolchain, and
// kernel headers for the running kernel), resolved per distro.
func awgBuildDeps() []string {
	switch {
	case commandExists("apt-get"):
		return []string{"dkms", "build-essential", awgKernelHeadersPackage()}
	case commandExists("dnf"), commandExists("yum"):
		return []string{"dkms", "gcc", "make", awgKernelHeadersPackage()}
	case commandExists("zypper"):
		return []string{"dkms", "gcc", "make", awgKernelHeadersPackage()}
	case commandExists("pacman"):
		return []string{"dkms", "base-devel", awgKernelHeadersPackage()}
	default:
		return nil
	}
}

// awgKernelHeadersPackage returns the header/devel package matching the running kernel,
// mirroring KernelModulesPackage. DKMS needs headers for `uname -r`.
func awgKernelHeadersPackage() string {
	switch {
	case commandExists("apt-get"):
		if r := runningKernel(); r != "" {
			return "linux-headers-" + r
		}
		if isUbuntu() {
			return "linux-headers-generic"
		}
		return "linux-headers-" + debKernelArch()
	case commandExists("dnf"), commandExists("yum"):
		if r := runningKernel(); r != "" {
			return "kernel-devel-" + r
		}
		return "kernel-devel"
	case commandExists("zypper"):
		return "kernel-default-devel"
	case commandExists("pacman"):
		return "linux-headers"
	default:
		return ""
	}
}

// awgHeaderFallbacks lists generic header packages to try if the exact running-kernel header
// package isn't available (box behind the archive, cut-down flavour).
func awgHeaderFallbacks(pkg string) []string {
	switch {
	case strings.HasPrefix(pkg, "linux-headers-") && isUbuntu():
		return []string{"linux-headers-generic"}
	case strings.HasPrefix(pkg, "linux-headers-"):
		return []string{"linux-headers-" + debKernelArch()}
	case strings.HasPrefix(pkg, "kernel-devel-"):
		return []string{"kernel-devel"}
	default:
		return nil
	}
}

// awgRunCmd runs a command and returns combined output + error (for the setup log).
func awgRunCmd(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Env = append(os.Environ(), "DEBIAN_FRONTEND=noninteractive")
	out, err := cmd.CombinedOutput()
	return string(out), err
}
