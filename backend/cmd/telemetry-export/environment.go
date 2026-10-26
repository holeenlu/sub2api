package main

import (
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

type observer struct {
	read    func(string) string
	command func(string, ...string) string
	env     func(string) string
	exists  func(string) bool
	cores   int
	mac     string
}

func realObserver() observer {
	mac := ""
	if interfaces, err := net.Interfaces(); err == nil {
		for _, iface := range interfaces {
			if iface.Flags&net.FlagLoopback == 0 && len(iface.HardwareAddr) > 0 && iface.HardwareAddr.String() != "00:00:00:00:00:00" {
				mac = iface.HardwareAddr.String()
				break
			}
		}
	}
	return observer{
		read: func(name string) string {
			raw, err := os.ReadFile(name)
			if err != nil {
				return ""
			}
			return strings.TrimSpace(string(raw))
		},
		command: func(name string, args ...string) string {
			file, err := exec.LookPath(name)
			if err != nil {
				return ""
			}
			raw, err := execute(file, args, "", nil, 5*time.Second, 1<<20)
			if err != nil {
				return ""
			}
			return strings.TrimSpace(string(raw))
		},
		env: os.Getenv, exists: func(name string) bool { _, err := exec.LookPath(name); return err == nil }, cores: runtime.NumCPU(), mac: mac,
	}
}

func first(values ...string) string {
	for _, v := range values {
		if v = strings.TrimSpace(v); v != "" {
			return v
		}
	}
	return ""
}
func number(facts map[string]any, key, value string, multiplier int64) {
	fields := strings.Fields(value)
	if len(fields) == 0 {
		return
	}
	if n, err := strconv.ParseInt(fields[0], 10, 64); err == nil && n >= 0 && n <= ((1<<53)-1)/multiplier {
		facts[key] = n * multiplier
	}
}
func collectEnvironment(platform string, o observer) (map[string]any, string) {
	facts := map[string]any{"shell": first(o.env("SHELL"), o.env("ComSpec"), "unknown"), "terminal": first(o.env("TERM_PROGRAM"), o.env("TERM"), "unknown"), "cpu_cores": o.cores}
	if o.env("WT_SESSION") != "" && o.env("TERM_PROGRAM") == "" {
		facts["terminal"] = "WindowsTerminal"
	}
	if o.mac != "" {
		facts["mac"] = o.mac
	}
	packages, runtimes := []string{}, []string{}
	for _, p := range [][2]string{{"apt-get", "apt"}, {"dnf", "dnf"}, {"yum", "yum"}, {"pacman", "pacman"}, {"zypper", "zypper"}, {"apk", "apk"}, {"nix-env", "nix"}, {"brew", "brew"}, {"winget", "winget"}, {"choco", "choco"}, {"scoop", "scoop"}} {
		if o.exists(p[0]) {
			packages = append(packages, p[1])
		}
	}
	seen := map[string]bool{}
	for _, p := range [][2]string{{"node", "node"}, {"bun", "bun"}, {"python3", "python"}, {"python", "python"}, {"go", "go"}, {"rustc", "rust"}, {"java", "java"}, {"ruby", "ruby"}, {"php", "php"}, {"deno", "deno"}, {"perl", "perl"}} {
		if o.exists(p[0]) && !seen[p[1]] {
			runtimes = append(runtimes, p[1])
			seen[p[1]] = true
		}
	}
	facts["package_managers"] = strings.Join(packages, ",")
	facts["runtimes"] = strings.Join(runtimes, ",")
	facts["vcs"] = ""
	if o.exists("git") {
		facts["vcs"] = "git"
	}
	locale := first(o.env("LC_ALL"), o.env("LC_MESSAGES"), o.env("LANG"))
	put := func(key, value string) {
		if value = strings.TrimSpace(value); value != "" {
			facts[key] = value
		}
	}
	switch platform {
	case "linux":
		put("linux_kernel", o.read("/proc/sys/kernel/osrelease"))
		for _, line := range strings.Split(o.read("/etc/os-release"), "\n") {
			key, value, ok := strings.Cut(line, "=")
			if !ok {
				continue
			}
			value = strings.Trim(value, "\"'")
			if key == "ID" {
				put("linux_distro_id", value)
			}
			if key == "PRETTY_NAME" {
				put("linux_distro_version", regexp.MustCompile(`[0-9]+(?:[.][0-9]+){0,2}`).FindString(value))
			}
		}
		for _, line := range strings.Split(o.read("/proc/cpuinfo"), "\n") {
			key, value, ok := strings.Cut(line, ":")
			if ok && (strings.TrimSpace(key) == "model name" || strings.TrimSpace(key) == "Hardware") {
				put("cpu_model", value)
				break
			}
		}
		for _, line := range strings.Split(o.read("/proc/meminfo"), "\n") {
			if v, ok := strings.CutPrefix(line, "MemTotal:"); ok {
				number(facts, "memory_bytes", v, 1024)
			}
		}
		for _, line := range strings.Split(o.read("/proc/mounts"), "\n") {
			f := strings.Fields(line)
			if len(f) > 1 && f[1] == "/" {
				put("root_disk", f[0])
				break
			}
		}
		for key, file := range map[string]string{"dmi_manufacturer": "sys_vendor", "dmi_product": "product_name", "dmi_version": "product_version", "dmi_family": "product_family", "dmi_serial": "product_serial", "dmi_uuid": "product_uuid"} {
			put(key, o.read("/sys/class/dmi/id/"+file))
		}
	case "darwin":
		put("cpu_model", o.command("sysctl", "-n", "machdep.cpu.brand_string"))
		number(facts, "memory_bytes", o.command("sysctl", "-n", "hw.memsize"), 1)
		hardware := o.command("ioreg", "-rd1", "-c", "IOPlatformExpertDevice")
		for key, name := range map[string]string{"dmi_serial": "IOPlatformSerialNumber", "dmi_uuid": "IOPlatformUUID", "dmi_product": "model"} {
			if match := regexp.MustCompile(`"` + name + `"[[:space:]]*=[[:space:]]*<?"([^"]+)"`).FindStringSubmatch(hardware); len(match) > 1 {
				put(key, match[1])
			}
		}
		if rows := strings.Split(o.command("df", "-P", "/"), "\n"); len(rows) > 1 {
			if f := strings.Fields(rows[1]); len(f) > 0 {
				put("root_disk", f[0])
			}
		}
		if locale == "" {
			locale = o.command("defaults", "read", "-g", "AppleLocale")
		}
	case "windows":
		// Fixed local CIM queries; no caller value is interpolated into PowerShell.
		raw := o.command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", `[Console]::OutputEncoding=[System.Text.UTF8Encoding]::new();$s=Get-CimInstance Win32_ComputerSystem;$p=Get-CimInstance Win32_ComputerSystemProduct;$c=Get-CimInstance Win32_Processor|Select-Object -First 1;[ordered]@{cpu_model=$c.Name;memory_bytes=$s.TotalPhysicalMemory;dmi_manufacturer=$s.Manufacturer;dmi_product=$s.Model;dmi_family=$s.SystemFamily;dmi_version=$p.Version;dmi_serial=$p.IdentifyingNumber;dmi_uuid=$p.UUID;locale=(Get-Culture).Name}|ConvertTo-Json -Compress`)
		var hw map[string]json.RawMessage
		if json.Unmarshal([]byte(raw), &hw) == nil {
			for _, key := range []string{"cpu_model", "dmi_manufacturer", "dmi_product", "dmi_family", "dmi_version", "dmi_serial", "dmi_uuid"} {
				var value string
				if json.Unmarshal(hw[key], &value) == nil {
					put(key, value)
				}
			}
			number(facts, "memory_bytes", string(hw["memory_bytes"]), 1)
			if locale == "" {
				_ = json.Unmarshal(hw["locale"], &locale)
			}
		}
		put("root_disk", o.env("SystemDrive"))
	}
	locale = strings.ReplaceAll(strings.Split(locale, ".")[0], "_", "-")
	if locale == "C" || locale == "POSIX" {
		locale = ""
	}
	return facts, locale
}
