package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
	"golang.org/x/sys/windows"
)

// Обновления берутся из релизов открытого репозитория на GitHub:
// у релиза тег vX.Y.Z и приложенный exe.
const updateRepo = "tthreeonek/t31k-binder"

type UpdateInfo struct {
	Available bool   `json:"available"`
	Current   string `json:"current"`
	Version   string `json:"version"`
	Notes     string `json:"notes"`
	Page      string `json:"page"`
}

type ghAsset struct {
	Name   string `json:"name"`
	Size   int64  `json:"size"`
	URL    string `json:"browser_download_url"`
	Digest string `json:"digest"` // "sha256:…", GitHub считает сам
}

type ghRelease struct {
	TagName string    `json:"tag_name"`
	Body    string    `json:"body"`
	HTMLURL string    `json:"html_url"`
	Assets  []ghAsset `json:"assets"`
}

var httpClient = &http.Client{Timeout: 20 * time.Second}

func fetchLatestRelease(ctx context.Context) (*ghRelease, error) {
	req, _ := http.NewRequestWithContext(ctx, "GET", "https://api.github.com/repos/"+updateRepo+"/releases/latest", nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "T31K-Binder/"+appVersion)
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, errors.New("нет связи с GitHub")
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, errors.New("релизов пока нет")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub ответил %d", resp.StatusCode)
	}
	var rel ghRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, errors.New("не удалось разобрать ответ GitHub")
	}
	return &rel, nil
}

// versionNewer — правда, если версия a новее b (сравнение по числам).
func versionNewer(a, b string) bool {
	pa := strings.Split(strings.TrimPrefix(a, "v"), ".")
	pb := strings.Split(strings.TrimPrefix(b, "v"), ".")
	for i := 0; i < max(len(pa), len(pb)); i++ {
		var x, y int
		if i < len(pa) {
			x, _ = strconv.Atoi(pa[i])
		}
		if i < len(pb) {
			y, _ = strconv.Atoi(pb[i])
		}
		if x != y {
			return x > y
		}
	}
	return false
}

func exeAsset(rel *ghRelease) *ghAsset {
	for i := range rel.Assets {
		if strings.HasSuffix(strings.ToLower(rel.Assets[i].Name), ".exe") {
			return &rel.Assets[i]
		}
	}
	return nil
}

// CheckUpdate спрашивает GitHub о последнем релизе.
func (a *App) CheckUpdate() (UpdateInfo, error) {
	info := UpdateInfo{Current: appVersion}
	rel, err := fetchLatestRelease(a.ctx)
	if err != nil {
		return info, err
	}
	info.Version = strings.TrimPrefix(rel.TagName, "v")
	info.Notes = rel.Body
	info.Page = rel.HTMLURL
	info.Available = versionNewer(rel.TagName, appVersion) && exeAsset(rel) != nil
	return info, nil
}

func (a *App) autoCheckUpdates() {
	time.Sleep(4 * time.Second)
	for {
		if a.snapshot().Settings.CheckUpdates {
			if info, err := a.CheckUpdate(); err == nil && info.Available {
				wruntime.EventsEmit(a.ctx, "binder:update", info)
			}
		}
		time.Sleep(6 * time.Hour)
	}
}

func (a *App) OpenURL(url string) {
	if strings.HasPrefix(url, "https://github.com/") {
		wruntime.BrowserOpenURL(a.ctx, url)
	}
}

// InstallUpdate скачивает новый exe, подменяет им текущий и перезапускает
// программу. Запущенный exe Windows переименовать разрешает, удалить — нет,
// поэтому старый файл остаётся рядом как .old и удаляется новой копией.
func (a *App) InstallUpdate() error {
	rel, err := fetchLatestRelease(a.ctx)
	if err != nil {
		return err
	}
	if !versionNewer(rel.TagName, appVersion) {
		return errors.New("уже установлена последняя версия")
	}
	asset := exeAsset(rel)
	if asset == nil {
		return errors.New("в релизе нет exe-файла")
	}

	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if p, err := filepath.EvalSymlinks(exe); err == nil {
		exe = p
	}
	newPath := exe + ".new"
	if err := a.download(asset, newPath); err != nil {
		_ = os.Remove(newPath)
		return err
	}

	oldPath := exe + ".old"
	_ = os.Remove(oldPath)
	if err := os.Rename(exe, oldPath); err != nil {
		_ = os.Remove(newPath)
		return errors.New("нет прав на папку программы — перенесите биндер в другую папку")
	}
	if err := os.Rename(newPath, exe); err != nil {
		_ = os.Rename(oldPath, exe)
		return errors.New("не удалось заменить файл: " + err.Error())
	}

	cmd := exec.Command(exe, "--updated", "--wait-pid", strconv.Itoa(os.Getpid()))
	cmd.Dir = filepath.Dir(exe)
	if err := cmd.Start(); err != nil {
		_ = os.Rename(exe, newPath)
		_ = os.Rename(oldPath, exe)
		return errors.New("не удалось запустить новую версию: " + err.Error())
	}
	a.rememberGeometry()
	go func() {
		time.Sleep(200 * time.Millisecond)
		wruntime.Quit(a.ctx)
	}()
	return nil
}

func (a *App) download(asset *ghAsset, path string) error {
	ctx, cancel := context.WithTimeout(a.ctx, 10*time.Minute)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", asset.URL, nil)
	req.Header.Set("User-Agent", "T31K-Binder/"+appVersion)
	client := &http.Client{} // без общего таймаута: файл может качаться долго
	resp, err := client.Do(req)
	if err != nil {
		return errors.New("не удалось скачать обновление")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("скачивание: GitHub ответил %d", resp.StatusCode)
	}
	f, err := os.Create(path)
	if err != nil {
		return errors.New("нет прав на папку программы — перенесите биндер в другую папку")
	}
	h := sha256.New()
	total := asset.Size
	var done int64
	lastPct := -1
	buf := make([]byte, 64*1024)
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := f.Write(buf[:n]); werr != nil {
				f.Close()
				return werr
			}
			h.Write(buf[:n])
			done += int64(n)
			if total > 0 {
				if pct := int(done * 100 / total); pct != lastPct {
					lastPct = pct
					wruntime.EventsEmit(a.ctx, "binder:update-progress", pct)
				}
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			f.Close()
			return errors.New("обрыв связи при скачивании")
		}
	}
	if err := f.Close(); err != nil {
		return err
	}
	if total > 0 && done != total {
		return errors.New("файл скачался не полностью")
	}
	if want, ok := strings.CutPrefix(asset.Digest, "sha256:"); ok {
		if got := hex.EncodeToString(h.Sum(nil)); !strings.EqualFold(got, want) {
			return errors.New("контрольная сумма не совпала — обновление отменено")
		}
	}
	return nil
}

// prepareAfterUpdate вызывается до запуска окна: ждёт, пока закроется
// прежняя копия (иначе сработает защита от второго экземпляра), и убирает
// оставшийся от неё .old.
func prepareAfterUpdate(args []string) (updated bool) {
	for i, arg := range args {
		switch arg {
		case "--updated":
			updated = true
		case "--wait-pid":
			if i+1 < len(args) {
				if pid, err := strconv.Atoi(args[i+1]); err == nil {
					waitProcessExit(uint32(pid), 15*time.Second)
				}
			}
		}
	}
	if exe, err := os.Executable(); err == nil {
		for i := 0; i < 10; i++ {
			if err := os.Remove(exe + ".old"); err == nil || os.IsNotExist(err) {
				break
			}
			time.Sleep(300 * time.Millisecond)
		}
	}
	return updated
}

func waitProcessExit(pid uint32, timeout time.Duration) {
	h, err := windows.OpenProcess(windows.SYNCHRONIZE, false, pid)
	if err != nil {
		return
	}
	defer windows.CloseHandle(h)
	_, _ = windows.WaitForSingleObject(h, uint32(timeout/time.Millisecond))
}
