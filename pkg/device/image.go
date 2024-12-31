//go:build !tinygo

package device

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

var (
	keepBuilds = Getenv("DEBUG_KEEP_BUILDS", "")
)

func setContentMd5(w http.ResponseWriter, fileName string) error {

	// Calculate MD5 checksum
	cmd := exec.Command("md5sum", fileName)
	LogDebug(cmd.String())
	stdoutStderr, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, stdoutStderr)
	}
	md5sum := bytes.Fields(stdoutStderr)[0]
	md5sumBase64 := base64.StdEncoding.EncodeToString(md5sum)

	// Set the MD5 checksum header
	w.Header().Set("Content-MD5", md5sumBase64)
	return nil
}

func serveFile(w http.ResponseWriter, r *http.Request, fileName string) error {

	if err := setContentMd5(w, fileName); err != nil {
		return err
	}

	w.Header().Set("Content-Disposition", "attachment; filename="+filepath.Base(fileName))
	w.Header().Set("Content-Type", "application/octet-stream")

	LogInfo("Serving download file", "name", fileName)
	http.ServeFile(w, r, fileName)

	return nil
}

func (d *device) genFile(dir, template, name string, data any) error {
	filePath := filepath.Join(dir, name)
	file, err := os.Create(filePath)
	if err != nil {
		return err
	}
	defer file.Close()
	return d.renderTmpl(file, template, data)
}

func isLocalhost(referer string) bool {
	url, _ := url.Parse(referer)
	hostname := url.Hostname()
	return hostname == "localhost" || hostname == "127.0.0.1" || hostname == "::1"
}

// createSFX concatenates the tar ball to the SFX installer script, making the
// SFX installer
func createSFX(dir, sfxFile, tarFile, installerFile string) error {

	script, err := os.Open(filepath.Join(dir, sfxFile))
	if err != nil {
		return fmt.Errorf("error opening sfx script file: %v", err)
	}
	defer script.Close()

	archive, err := os.Open(filepath.Join(dir, tarFile))
	if err != nil {
		return fmt.Errorf("error opening archive file: %v", err)
	}
	defer archive.Close()

	installer, err := os.Create(filepath.Join(dir, installerFile))
	if err != nil {
		return fmt.Errorf("error creating installer file: %v", err)
	}
	defer installer.Close()

	// Copy the script file to the installer file
	if _, err := io.Copy(installer, script); err != nil {
		return fmt.Errorf("error writing script to installer file: %v", err)
	}

	// Copy the archive file to the installer file
	if _, err := io.Copy(installer, archive); err != nil {
		return fmt.Errorf("error writing archive to installer file: %v", err)
	}

	return nil
}

func (d *device) buildLinuxImage(w http.ResponseWriter, r *http.Request, dir string) error {

	referer := r.Referer()
	if isLocalhost(referer) {
		return fmt.Errorf("Cannot use localhost for hub address.  Access the hub " +
			"using the hostname or IP address of the host; something that " +
			"is addressable on the network so the device can dial into the hub.")
	}

	var service = d.Model + "-" + d.Id
	var dialurls = strings.Replace(referer, "http", "ws", 1) + "ws"

	// Generate environment variable file.  The service will load env vars
	// from this file.
	if err := d.genFile(dir, "device-env.tmpl", "env", map[string]any{
		"port":     r.URL.Query().Get("port"),
		"user":     Getenv("USER", ""),
		"passwd":   Getenv("PASSWD", ""),
		"dialurls": dialurls,
	}); err != nil {
		return err
	}

	// Generate systemd merliot.target unit from
	// device-merliot-target.tmpl.  This will be the parent unit of all
	// device units.
	targetFile := "merliot.target"
	if err := d.genFile(dir, "device-merliot-target.tmpl", targetFile, nil); err != nil {
		return err
	}

	// Generate systemd {{service}}.service unit from device-service.tmpl
	serviceFile := service + ".service"
	if err := d.genFile(dir, "device-service.tmpl", serviceFile, map[string]any{
		"service": service,
	}); err != nil {
		return err
	}

	// Generate {{service}}.conf from device-conf.tmpl.  This sets up
	// logging service for the device.  Logs are available at
	// /var/log/{{.service}}.log.
	confFile := service + ".conf"
	if err := d.genFile(dir, "device-conf.tmpl", confFile, map[string]any{
		"service": service,
	}); err != nil {
		return err
	}

	// Generate SelF-eXtracting (SFX) installer script
	sfxFile := "sfx.sh"
	if err := d.genFile(dir, "device-sfx.tmpl", sfxFile, map[string]any{
		"service": service,
	}); err != nil {
		return err
	}

	// Generate service install script
	if err := d.genFile(dir, "device-install.tmpl", "install.sh", map[string]any{
		"service": service,
	}); err != nil {
		return err
	}

	// Make a devices.json file
	if err := fileWriteJSON(filepath.Join(dir, "devices.json"), d.devices()); err != nil {
		return err
	}

	// TODO only tar up bin/ files that are needed for the target device

	// Create a gzipped tar ball with everything inside need to
	// install/uninstall the device
	tarFile := service + ".tar.gz"
	tarFilePath := filepath.Join(dir, tarFile)
	cmd := exec.Command("tar",
		"--exclude", tarFile,
		"-czf", tarFilePath,
		"-C", ".", "bin/",
		"-C", dir, ".")
	LogDebug(cmd.String())
	stdoutStderr, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, stdoutStderr)
	}

	// Create final SFX image as the installer

	installer := service + "-installer"
	if err := createSFX(dir, sfxFile, tarFile, installer); err != nil {
		return err
	}

	// Serve installer file for download

	return serveFile(w, r, filepath.Join(dir, installer))
}

func (d *device) buildTinyGoImage(w http.ResponseWriter, r *http.Request, dir, target string) error {

	var referer = r.Referer()
	if isLocalhost(referer) {
		return fmt.Errorf("Cannot use localhost for hub.  Use the hub " +
			"hostname or IP address; something that is addressable so the " +
			"device can dial into the hub.")
	}

	var dialurls = strings.Replace(referer, "http", "ws", 1) + "ws"
	var ssid = r.URL.Query().Get("ssid")
	var wifiAuths = wifiAuths()
	var passphrase = wifiAuths[ssid]

	var p = uf2ParamsBlock{
		MagicStart: uf2Magic,
		uf2Params: uf2Params{
			Ssid:         ssid,
			Passphrase:   passphrase,
			Id:           d.Id,
			Model:        d.Model,
			Name:         d.Name,
			DeployParams: d.DeployParams,
			User:         Getenv("USER", ""),
			Passwd:       Getenv("PASSWD", ""),
			DialURLs:     dialurls,
		},
		MagicEnd: uf2Magic,
	}

	base := filepath.Join("uf2s", d.Model+"-"+target+".uf2")
	installer := filepath.Join(dir, d.Model+"-"+d.Id+"-installer.uf2")

	if err := uf2Create(base, installer, p); err != nil {
		return err
	}

	LogInfo("Built Tinygo device image", "installer", installer)
	return serveFile(w, r, installer)
}

func (d *device) buildImage(w http.ResponseWriter, r *http.Request) error {

	// Create temp build directory
	dir, err := os.MkdirTemp("./", d.Model+"-"+d.Id+"-")
	if err != nil {
		return err
	}

	if keepBuilds != "" {
		LogDebug("Temporary build", "dir", dir)
	} else {
		defer os.RemoveAll(dir)
	}

	target := r.URL.Query().Get("target")

	switch target {
	case "x86-64", "rpi":
		return d.buildLinuxImage(w, r, dir)
	case "nano-rp2040", "wioterminal", "pyportal":
		return d.buildTinyGoImage(w, r, dir, target)
	default:
		return fmt.Errorf("Target '%s' not supported", target)
	}

	return nil
}

func (d *device) downloadMsgClear(sessionId string) {
	var buf bytes.Buffer
	if err := d.renderTmpl(&buf, "device-download-msg-empty.tmpl", nil); err != nil {
		LogError("Rendering template", "err", err)
		return
	}
	sessionSend(sessionId, string(buf.Bytes()))
}

func (d *device) downloadMsgError(sessionId string, downloadErr error) {
	var buf bytes.Buffer
	if err := d.renderTmpl(&buf, "device-download-msg-error.tmpl", map[string]any{
		"err": "Download error: " + downloadErr.Error(),
	}); err != nil {
		LogError("Rendering template", "err", err)
		return
	}
	sessionSend(sessionId, string(buf.Bytes()))
}

func (d *device) downloadImage(w http.ResponseWriter, r *http.Request) {

	var sessionId = r.PathValue("sessionId")

	d.downloadMsgClear(sessionId)

	if d.IsSet(flagLocked) {
		err := fmt.Errorf("Refusing to download: device is locked")
		d.downloadMsgError(sessionId, err)
		w.WriteHeader(http.StatusNoContent)
		return
	}

	// The r.URL values are passed in from the download <form>.  These
	// values are the proposed new device config, and should decode into
	// the device.  If accepted, the device is updated and the config is
	// stored in DeployParams.

	changed, err := d.formConfig(r.URL.RawQuery)
	if err != nil {
		d.downloadMsgError(sessionId, err)
		w.WriteHeader(http.StatusNoContent)
		return
	}

	// Built it!

	if err := d.buildImage(w, r); err != nil {
		d.downloadMsgError(sessionId, err)
		w.WriteHeader(http.StatusNoContent)
		return
	}

	// If the device config has changed, kick the downlink device offline.
	// It will try to reconnect, but fail, because the DeployParams now
	// don't match this (uplink) device.  Once the downlink device is
	// updated (with the image we created above) the downlink device
	// will connect.

	if changed {
		deviceDirty(root.Id)
		downlinkClose(d.Id)
	}
}
