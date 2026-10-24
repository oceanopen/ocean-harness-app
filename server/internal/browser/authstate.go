package browser

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

/*
 * 工作空间共享登录态（D3 修订：凭据仅此一份落盘，不进数据库）：数据模型镜像 Playwright
 * storageState JSON（引擎工具导出/注入原生格式，零转换）；merge 为并集语义——引擎侧
 * （新）覆盖同名项、共享文件（旧）独有项保留，防同工作空间并行会话互相覆盖丢失。
 */

// StorageCookie Playwright storageState 的 cookie 项（expires -1 = 会话期）。
type StorageCookie struct {
	Name     string  `json:"name"`
	Value    string  `json:"value"`
	Domain   string  `json:"domain"`
	Path     string  `json:"path"`
	Expires  float64 `json:"expires"`
	HTTPOnly bool    `json:"httpOnly"`
	Secure   bool    `json:"secure"`
	SameSite string  `json:"sameSite"`
}

// StorageKV localStorage 键值项。
type StorageKV struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// StorageOrigin 单 origin 的 localStorage 快照。
type StorageOrigin struct {
	Origin       string      `json:"origin"`
	LocalStorage []StorageKV `json:"localStorage"`
}

// StorageState Playwright storageState 全量（cookies + origins）。
type StorageState struct {
	Cookies []StorageCookie `json:"cookies"`
	Origins []StorageOrigin `json:"origins"`
}

// cookieKey 合并判重键（同 cookie 标识三要素）。
type cookieKey struct{ name, domain, path string }

func cookieExpired(c StorageCookie, now time.Time) bool {
	return c.Expires > 0 && c.Expires < float64(now.Unix())
}

/*
 * mergeStorageState 并集合并：cookie 按 (name, domain, path)、localStorage 按
 * (origin, name) 判重，engine 侧覆盖、shared 独有保留；双侧过期 cookie 剔除；
 * shared 序为基、engine 新项追加（同输入同输出，供注入跳重 hash 判定）。
 */
func mergeStorageState(shared, engine StorageState, now time.Time) StorageState {
	engineCookies := map[cookieKey]StorageCookie{}
	for _, c := range engine.Cookies {
		engineCookies[cookieKey{c.Name, c.Domain, c.Path}] = c
	}

	merged := StorageState{}
	for _, c := range shared.Cookies {
		key := cookieKey{c.Name, c.Domain, c.Path}
		if ec, ok := engineCookies[key]; ok {
			delete(engineCookies, key)
			c = ec
		}
		if !cookieExpired(c, now) {
			merged.Cookies = append(merged.Cookies, c)
		}
	}
	for _, c := range engine.Cookies {
		if _, ok := engineCookies[cookieKey{c.Name, c.Domain, c.Path}]; ok && !cookieExpired(c, now) {
			merged.Cookies = append(merged.Cookies, c)
		}
	}

	engineOrigins := map[string]StorageOrigin{}
	for _, o := range engine.Origins {
		engineOrigins[o.Origin] = o
	}
	for _, so := range shared.Origins {
		eo, hasEngine := engineOrigins[so.Origin]
		if hasEngine {
			delete(engineOrigins, so.Origin)
			so.LocalStorage = mergeKV(so.LocalStorage, eo.LocalStorage)
		}
		merged.Origins = append(merged.Origins, so)
	}
	for _, o := range engine.Origins {
		if _, ok := engineOrigins[o.Origin]; ok {
			merged.Origins = append(merged.Origins, o)
		}
	}
	return merged
}

// mergeKV localStorage 键值并集（engine 覆盖同名、shared 独有保留，shared 序为基）。
func mergeKV(shared, engine []StorageKV) []StorageKV {
	engineKV := map[string]string{}
	for _, kv := range engine {
		engineKV[kv.Name] = kv.Value
	}
	merged := make([]StorageKV, 0, len(shared)+len(engine))
	for _, kv := range shared {
		if v, ok := engineKV[kv.Name]; ok {
			delete(engineKV, kv.Name)
			kv.Value = v
		}
		merged = append(merged, kv)
	}
	for _, kv := range engine {
		if _, ok := engineKV[kv.Name]; ok {
			merged = append(merged, kv)
		}
	}
	return merged
}

/*
 * readSharedState 读取共享登录态：exists=false 为文件缺失（调用方决定跳过注入或按空
 * 合并）；损坏 JSON 按空态返回并带回错误（仅路径不含内容，供日志）。
 */
func readSharedState(path string) (state StorageState, exists bool, err error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return StorageState{}, false, nil
	}
	if err != nil {
		return StorageState{}, true, fmt.Errorf("读取共享登录态: %w", err)
	}
	if err := json.Unmarshal(data, &state); err != nil {
		return StorageState{}, true, fmt.Errorf("共享登录态 %s 解析失败（按空态处理）: %w", filepath.Base(path), err)
	}
	return state, true, nil
}

/*
 * writeSharedStateAtomic 原子写共享登录态（同目录 tmp 0600 + rename；调用方须持
 * 共享文件锁，见 withSharedFile）。
 */
func writeSharedStateAtomic(path string, state StorageState) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("创建共享登录态目录: %w", err)
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化共享登录态: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".auth-state-*.tmp")
	if err != nil {
		return fmt.Errorf("创建共享登录态临时文件: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // rename 成功后为 no-op
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("写入共享登录态临时文件: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("关闭共享登录态临时文件: %w", err)
	}
	if err := os.Chmod(tmpName, 0o600); err != nil {
		return fmt.Errorf("共享登录态临时文件权限: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("替换共享登录态文件: %w", err)
	}
	return nil
}

/*
 * sharedFileLock 共享登录态文件写锁：同进程多会话回写同一全局文件时串行化
 * 读-合并-写（跨进程无第二写者，Rust 不触该目录）。
 */
var sharedFileLock sync.Mutex

// withSharedFile 持共享文件锁执行 fn（合并写纪律单点）。
func withSharedFile(fn func()) {
	sharedFileLock.Lock()
	defer sharedFileLock.Unlock()
	fn()
}
