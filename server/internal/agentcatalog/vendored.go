package agentcatalog

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"ocean-harness/server/internal/clibin"

	"ocean-harness/server/internal/acp"
)

// Vendoring（T1.3）：adapter 依赖在构建期 pnpm 安装随应用打包（资源源，
// scripts/prepare-acp-adapters.ts 产出），运行期按需复制进受管目录后以
// `node <vendored 入口>` 拉起——运行时零 npm / registry 依赖（公司网络约束），
// 版本由 catalog pin 在构建期锁定。两目录布局同构，均 <root>/<id>/<version>/：
//
//	资源源 GO_SERVER_ACP_RESOURCES_DIR：打包只读内容
//	受管根 GO_SERVER_ACP_ADAPTERS_DIR：EnsureVendored 自建并保持卫生

// vendoredMarker 安装完整性 marker，位于安装目录根部，内容 = 条目版本号。
const vendoredMarker = ".ocean-vendored"

// vendoredMu 序列化 EnsureVendored：并发拉起会话时避免对同一受管树并行安装。
var vendoredMu sync.Mutex

// EnsureVendored 确保条目 adapter 在受管目录就绪，返回安装目录 <dstRoot>/<id>/<version>。
//
// 幂等：marker 内容 = 版本即命中复用；缺失或不符（半成品、内容漂移）整树重制。
// 安装走「临时目录 + rename」原子落地，marker 先于 rename 写入（rename 可见即完整安装）。
// 成功后清理同 id 下其它版本目录与临时残留（受管目录恒只保留当前 pin 版本）。
// 仅支持 npx-adapter 策略；srcRoot / dstRoot 为空 = 未配置，显式报错（config 可选字段
// 语义：不阻断启动，本函数调用期拦截）。
func EnsureVendored(entry Entry, srcRoot, dstRoot string) (string, error) {
	if entry.Strategy != StrategyNpxAdapter {
		return "", fmt.Errorf("agent %q 策略 %s 无 vendoring 形态", entry.ID, entry.Strategy)
	}
	if srcRoot == "" || dstRoot == "" {
		return "", errors.New("ACP vendoring 目录未配置（资源源与受管根均不可为空）")
	}
	src := filepath.Join(srcRoot, entry.ID, entry.Version)
	dst := filepath.Join(dstRoot, entry.ID, entry.Version)

	vendoredMu.Lock()
	defer vendoredMu.Unlock()

	// 命中：marker 内容 = 版本即视为完整安装，直接复用。
	if data, err := os.ReadFile(filepath.Join(dst, vendoredMarker)); err == nil &&
		strings.TrimSpace(string(data)) == entry.Version {
		return dst, nil
	}
	if info, err := os.Stat(src); err != nil || !info.IsDir() {
		return "", fmt.Errorf("内置 ACP adapter 资源缺失: %s（请确认应用打包包含 acp-adapters 资源）", src)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return "", fmt.Errorf("创建受管目录: %w", err)
	}
	tmp, err := os.MkdirTemp(filepath.Dir(dst), "."+entry.Version+".tmp-")
	if err != nil {
		return "", fmt.Errorf("创建安装临时目录: %w", err)
	}
	if err := copyTree(src, tmp); err != nil {
		_ = os.RemoveAll(tmp)
		return "", fmt.Errorf("复制 vendored 资源: %w", err)
	}
	if err := os.WriteFile(filepath.Join(tmp, vendoredMarker), []byte(entry.Version), 0o644); err != nil {
		_ = os.RemoveAll(tmp)
		return "", fmt.Errorf("写入 vendored marker: %w", err)
	}
	// marker 不符的既有 dst 不可信（半成品/漂移），整树替换。
	if err := os.RemoveAll(dst); err != nil {
		_ = os.RemoveAll(tmp)
		return "", fmt.Errorf("清理既有安装目录: %w", err)
	}
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.RemoveAll(tmp)
		return "", fmt.Errorf("原子落地 vendored 安装: %w", err)
	}
	// 受管卫生：同 id 只保留当前版本（旧版本目录、中断残留的 .tmp-* 一并清除）。
	if siblings, err := os.ReadDir(filepath.Dir(dst)); err == nil {
		for _, s := range siblings {
			if s.Name() != entry.Version {
				_ = os.RemoveAll(filepath.Join(filepath.Dir(dst), s.Name()))
			}
		}
	}
	return dst, nil
}

// VendoredSpawnConfig 将 npx-adapter 条目翻译为指向 vendored 入口的 acp.SpawnConfig
// （argv = node <包 bin 入口>，installDir 为 EnsureVendored 返回值；node 经 resolveNodeBin
// 落成绝对路径）。claude 专属注入（可执行文件指定 / login PATH）仍归调用方经 Env 追加，
// 与 SpawnConfig 同约定（D5）。native-acp 条目无 vendored 形态（走 SpawnConfig 直连），
// 此处显式报错防误用。
func (e Entry) VendoredSpawnConfig(installDir, cwd string) (acp.SpawnConfig, error) {
	if e.Strategy != StrategyNpxAdapter {
		return acp.SpawnConfig{}, fmt.Errorf("agent %q 策略 %s 无 vendored 入口形态", e.ID, e.Strategy)
	}
	nodeBin, err := ResolveNodeBin()
	if err != nil {
		return acp.SpawnConfig{}, fmt.Errorf("agent %q %w", e.ID, err)
	}
	pkgName, err := adapterPkgName(e.Args)
	if err != nil {
		return acp.SpawnConfig{}, fmt.Errorf("agent %q %w", e.ID, err)
	}
	pkgDir := filepath.Join(installDir, "node_modules", filepath.FromSlash(pkgName))
	binRel, err := readPkgBinEntry(pkgDir, pkgName)
	if err != nil {
		return acp.SpawnConfig{}, err
	}
	entryPath := filepath.Join(pkgDir, filepath.FromSlash(binRel))
	if _, err := os.Stat(entryPath); err != nil {
		return acp.SpawnConfig{}, fmt.Errorf("vendored 入口不存在: %s", entryPath)
	}
	return acp.SpawnConfig{
		Command: []string{nodeBin, entryPath},
		Env:     maps.Clone(e.Env),
		Cwd:     cwd,
	}, nil
}

// ResolveNodeBin 解析 node 可执行文件绝对路径（vendored spawn 翻译与 T1.4 doctor 探测
// 共用，单一 SSOT）：exec.Command 对相对 argv[0] 按 sidecar 自身 PATH 查找，
// SpawnConfig.Env 的覆盖不影响查找——GUI 拉起的 sidecar PATH 又常缺 nvm/volta 目录
// （claude 同源问题），故 LookPath 失败后沿 login PATH 逐目录兜底。落成绝对路径后，
// 拉起不受 spawn env 的 PATH 覆盖影响。
func ResolveNodeBin() (string, error) {
	if p, err := exec.LookPath("node"); err == nil {
		return p, nil
	}
	loginPath, err := clibin.LoginPath()
	if err != nil {
		return "", fmt.Errorf("解析 node 可执行文件: %w", err)
	}
	bin, err := nodeBinFromDirs(filepath.SplitList(loginPath))
	if err != nil {
		return "", fmt.Errorf("解析 node 可执行文件（PATH 与 login shell 均未命中）: %w", err)
	}
	return bin, nil
}

// nodeBinFromDirs 沿目录列表找首个可执行 node（Windows 命中 node.exe；执行位判定
// 对齐 clibin 的安装位置兜底）。
func nodeBinFromDirs(dirs []string) (string, error) {
	names := []string{"node"}
	if runtime.GOOS == "windows" {
		names = append(names, "node.exe")
	}
	for _, dir := range dirs {
		for _, name := range names {
			p := filepath.Join(dir, name)
			if info, err := os.Stat(p); err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
				return p, nil
			}
		}
	}
	return "", errors.New("未找到可执行 node")
}

// adapterPkgName 从条目 spawn args 还原 npm 包名（scripts/prepare-acp-adapters.ts 同款
// 规则：最后一个非 flag 参数为安装 spec，去掉 @版本尾缀）。条目形如 ["-y", "@scope/pkg@1.2.3"]。
func adapterPkgName(args []string) (string, error) {
	spec := ""
	for i := len(args) - 1; i >= 0; i-- {
		if !strings.HasPrefix(args[i], "-") {
			spec = args[i]
			break
		}
	}
	if spec == "" {
		return "", errors.New("args 无安装包 spec（非 flag 参数）")
	}
	if idx := strings.LastIndex(spec, "@"); idx > 0 {
		spec = spec[:idx]
	}
	return spec, nil
}

// readPkgBinEntry 读安装包 package.json 的 bin 字段并解析出相对入口路径。支持 npm 两种
// 形态：字符串（唯一 bin）；map 单键直取、多键取与包名尾段同名键（npm 命名惯例）。
func readPkgBinEntry(pkgDir, pkgName string) (string, error) {
	data, err := os.ReadFile(filepath.Join(pkgDir, "package.json"))
	if err != nil {
		return "", fmt.Errorf("读取 vendored 包 package.json: %w", err)
	}
	var manifest struct {
		Bin json.RawMessage `json:"bin"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		return "", fmt.Errorf("解析 vendored 包 package.json: %w", err)
	}
	var binStr string
	if err := json.Unmarshal(manifest.Bin, &binStr); err == nil && binStr != "" {
		return binStr, nil
	}
	var binMap map[string]string
	if err := json.Unmarshal(manifest.Bin, &binMap); err != nil || len(binMap) == 0 {
		return "", fmt.Errorf("vendored 包 %s 无合法 bin 字段", pkgName)
	}
	if len(binMap) == 1 {
		for _, v := range binMap {
			return v, nil
		}
	}
	if v, ok := binMap[filepath.Base(pkgName)]; ok {
		return v, nil
	}
	keys := make([]string, 0, len(binMap))
	for k := range binMap {
		keys = append(keys, k)
	}
	return "", fmt.Errorf("vendored 包 %s bin 为多键 map 且无同名键: %v", pkgName, keys)
}

// copyTree 递归复制目录树：目录与文件按源权限位重建，symlink 原样重建（npm 的 .bin
// 为相对链，两端布局同构即有效）；其余类型（socket 等）跳过。
func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		switch {
		case d.Type()&fs.ModeSymlink != 0:
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			return os.Symlink(link, target)
		case d.IsDir():
			info, err := d.Info()
			if err != nil {
				return err
			}
			return os.MkdirAll(target, info.Mode().Perm())
		case d.Type().IsRegular():
			info, err := d.Info()
			if err != nil {
				return err
			}
			return copyFile(path, target, info.Mode().Perm())
		default:
			return nil
		}
	})
}

// copyFile 复制单个文件并按源权限位落盘（WriteFile 经 umask 可能削掉执行位，Chmod 显式补齐）。
func copyFile(src, dst string, perm fs.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Chmod(dst, perm)
}
