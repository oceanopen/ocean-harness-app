package service

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"ocean-harness/src-server/internal/apis"
	"ocean-harness/src-server/internal/dal/model"
	"ocean-harness/src-server/internal/dal/query"
	"ocean-harness/src-server/internal/dal/types"
	"ocean-harness/src-server/internal/gitutil"
)

// LocalRepository 对应 /api/localRepository 命名空间下的业务逻辑（从 src-tauri 迁移而来）。
// 嵌入 apis.Service 获得由 controller 灌入的 Context/Orm/Logger；方法只收 req、用 svc.Orm、返响应 DTO。
type LocalRepository struct {
	apis.Service
}

// GetList 返回全部本地仓库，按 last_commit_at 倒序、id 升序（与原 Rust 排序一致）。
func (svc LocalRepository) GetList() ([]types.LocalRepositoryResponseData, error) {
	return svc.findAll()
}

// GetInfo 按 id 返回单个仓库。
func (svc LocalRepository) GetInfo(req *types.LocalRepositoryGetInfoRequest) (types.LocalRepositoryResponseData, error) {
	q := query.Use(svc.Orm)
	r, err := q.LocalRepository.WithContext(svc.Context).Where(q.LocalRepository.ID.Eq(req.ID)).First()
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return types.LocalRepositoryResponseData{}, errors.New("仓库不存在")
		}
		return types.LocalRepositoryResponseData{}, err
	}
	return types.LocalRepositoryResponseData{}.FromModel(r), nil
}

// Create 新建仓库。严格校验：name/localDir 非空、localDir 为存在的绝对路径且为 git 仓库；
// localDir 唯一（重复返回「该仓库目录已添加过」）；subDirList 每项拼接目录须存在（否则「子目录路径无效或不存在」）。
// 校验通过后解析 git 信息入库，返回新建仓库。
func (svc LocalRepository) Create(req *types.LocalRepositoryCreateRequest) (types.LocalRepositoryResponseData, error) {
	name := strings.TrimSpace(req.Name)
	localDir := strings.TrimSpace(req.LocalDir)
	if name == "" {
		return types.LocalRepositoryResponseData{}, errors.New("仓库名称不能为空")
	}
	if localDir == "" {
		return types.LocalRepositoryResponseData{}, errors.New("仓库目录不能为空")
	}
	if err := validateRepoDir(localDir); err != nil {
		return types.LocalRepositoryResponseData{}, err
	}
	subDirs, err := normalizeSubDirs(localDir, req.SubDirList)
	if err != nil {
		return types.LocalRepositoryResponseData{}, err
	}
	description := capDescription(req.Description)

	info := gitutil.ParseInfo(localDir)
	subDirJSON, _ := json.Marshal(subDirs) // []RepoSubDir → JSON 文本，该结构序列化不会失败

	q := query.Use(svc.Orm)
	// localDir 唯一性（DB 也有 UNIQUE 索引兜底）。
	count, err := q.LocalRepository.WithContext(svc.Context).Where(q.LocalRepository.LocalDir.Eq(localDir)).Count()
	if err != nil {
		return types.LocalRepositoryResponseData{}, err
	}
	if count > 0 {
		return types.LocalRepositoryResponseData{}, errors.New("该仓库目录已添加过")
	}

	repo := &model.LocalRepository{
		Name:        name,
		LocalDir:    localDir,
		Description: description,
		SubDirList:  string(subDirJSON),
	}
	applyGitInfo(repo, info)
	if err := q.LocalRepository.WithContext(svc.Context).Create(repo); err != nil {
		return types.LocalRepositoryResponseData{}, err
	}
	return types.LocalRepositoryResponseData{}.FromModel(repo), nil
}

// Update 更新仓库的 name/localDir/description/subDirList。校验新 localDir 须为 git 仓库且不与其他记录重复；
// subDirList 每项拼接目录须存在。校验通过后重新解析 git 信息并更新，返回更新后的仓库。
func (svc LocalRepository) Update(req *types.LocalRepositoryUpdateRequest) (types.LocalRepositoryResponseData, error) {
	name := strings.TrimSpace(req.Name)
	localDir := strings.TrimSpace(req.LocalDir)
	if name == "" {
		return types.LocalRepositoryResponseData{}, errors.New("仓库名称不能为空")
	}
	if localDir == "" {
		return types.LocalRepositoryResponseData{}, errors.New("仓库目录不能为空")
	}
	if err := validateRepoDir(localDir); err != nil {
		return types.LocalRepositoryResponseData{}, err
	}
	subDirs, err := normalizeSubDirs(localDir, req.SubDirList)
	if err != nil {
		return types.LocalRepositoryResponseData{}, err
	}
	description := capDescription(req.Description)

	info := gitutil.ParseInfo(localDir)
	subDirJSON, _ := json.Marshal(subDirs)

	q := query.Use(svc.Orm)
	// localDir 唯一性：排除自身记录。
	count, err := q.LocalRepository.WithContext(svc.Context).
		Where(q.LocalRepository.LocalDir.Eq(localDir), q.LocalRepository.ID.Neq(req.ID)).Count()
	if err != nil {
		return types.LocalRepositoryResponseData{}, err
	}
	if count > 0 {
		return types.LocalRepositoryResponseData{}, errors.New("该仓库目录已添加过")
	}

	r, err := q.LocalRepository.WithContext(svc.Context).Where(q.LocalRepository.ID.Eq(req.ID)).First()
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return types.LocalRepositoryResponseData{}, errors.New("仓库不存在")
		}
		return types.LocalRepositoryResponseData{}, err
	}
	r.Name = name
	r.LocalDir = localDir
	r.Description = description
	r.SubDirList = string(subDirJSON)
	applyGitInfo(r, info)
	if err := q.LocalRepository.WithContext(svc.Context).Save(r); err != nil {
		return types.LocalRepositoryResponseData{}, err
	}
	return types.LocalRepositoryResponseData{}.FromModel(r), nil
}

// Delete 物理删除仓库（释放 localDir 供重新添加）。
// 事务内级联清理（避免悬挂引用）：硬删中间表 t_project_local_repositories 关联记录 +
// 硬删 issue↔仓库关联表 t_issue_local_repositories 中指向该仓库的记录。
func (svc LocalRepository) Delete(req *types.LocalRepositoryDeleteRequest) error {
	return svc.Orm.Transaction(func(tx *gorm.DB) error {
		q := query.Use(tx)
		// 1) 物理删除仓库。
		if _, e := q.LocalRepository.WithContext(svc.Context).Where(q.LocalRepository.ID.Eq(req.ID)).Delete(); e != nil {
			return e
		}
		// 2) 硬删中间表关联记录（项目↔仓库）。
		if _, e := q.ProjectLocalRepository.WithContext(svc.Context).
			Where(q.ProjectLocalRepository.LocalRepositoryID.Eq(req.ID)).Delete(); e != nil {
			return e
		}
		// 3) 硬删 issue↔仓库关联记录（关联表无归属维度，按仓库 id 全局清）。
		_, e := q.IssueLocalRepository.WithContext(svc.Context).
			Where(q.IssueLocalRepository.LocalRepositoryID.Eq(req.ID)).Delete()
		return e
	})
}

// Refresh 重解析单个仓库的 git 信息并更新，返回新数据。
// Go HTTP 单请求独立 service/Orm，无 Rust 的 Mutex；SQLite 单连接由 gorm 自动串行化写。
func (svc LocalRepository) Refresh(req *types.LocalRepositoryRefreshRequest) (types.LocalRepositoryResponseData, error) {
	q := query.Use(svc.Orm)
	r, err := q.LocalRepository.WithContext(svc.Context).Where(q.LocalRepository.ID.Eq(req.ID)).First()
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return types.LocalRepositoryResponseData{}, errors.New("仓库不存在")
		}
		return types.LocalRepositoryResponseData{}, err
	}
	applyGitInfo(r, gitutil.ParseInfo(r.LocalDir))
	if err := q.LocalRepository.WithContext(svc.Context).Save(r); err != nil {
		return types.LocalRepositoryResponseData{}, err
	}
	return types.LocalRepositoryResponseData{}.FromModel(r), nil
}

// RefreshAll 遍历重解析全部仓库并更新，返回 DB 最新列表（保证 UI 与 DB 一致）。
// 串行解析（单用户场景 git 串行可接受）；部分仓库 Save 失败仅记日志不中断，已成功的更新落库。
func (svc LocalRepository) RefreshAll() ([]types.LocalRepositoryResponseData, error) {
	q := query.Use(svc.Orm)
	repos, err := q.LocalRepository.WithContext(svc.Context).Find()
	if err != nil {
		return nil, err
	}
	for _, r := range repos {
		applyGitInfo(r, gitutil.ParseInfo(r.LocalDir))
		if e := q.LocalRepository.WithContext(svc.Context).Save(r); e != nil {
			if svc.Logger != nil {
				svc.Logger.Warn("[localRepository] refresh update failed", zap.Int("id", r.ID), zap.Error(e))
			}
		}
	}
	// 重新查询保证返回与 DB 一致（避免内存态与落库态偏差）。
	return svc.findAll()
}

// GetLocalBranches 列出仓库的本地分支名（git branch 默认仅本地分支）。
// 仓库不存在返回错误；非 git 目录 / 无分支返回空切片（gitutil 留空，不阻塞前端选择器）。
func (svc LocalRepository) GetLocalBranches(req *types.LocalRepositoryGetLocalBranchesRequest) ([]string, error) {
	q := query.Use(svc.Orm)
	r, err := q.LocalRepository.WithContext(svc.Context).Where(q.LocalRepository.ID.Eq(req.ID)).First()
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("仓库不存在")
		}
		return nil, err
	}
	return gitutil.LocalBranches(r.LocalDir), nil
}

// findAll 查询全部仓库并装配响应（GetList/RefreshAll 共用）。
func (svc LocalRepository) findAll() ([]types.LocalRepositoryResponseData, error) {
	q := query.Use(svc.Orm)
	repos, err := q.LocalRepository.WithContext(svc.Context).
		Order(q.LocalRepository.LastCommitAt.Desc()).
		Order(q.LocalRepository.ID.Asc()).
		Find()
	if err != nil {
		return nil, err
	}
	out := make([]types.LocalRepositoryResponseData, 0, len(repos))
	for _, r := range repos {
		out = append(out, types.LocalRepositoryResponseData{}.FromModel(r))
	}
	return out, nil
}

// applyGitInfo 把解析结果写入驻入的 DO 指针。
func applyGitInfo(r *model.LocalRepository, info gitutil.Info) {
	r.RemoteURL = info.RemoteURL
	r.CurrentBranch = info.Branch
	r.DefaultBranch = info.DefaultBranch
	r.LastCommitAt = int(info.LastCommitAt) // DO LastCommitAt 为 int（64 位平台等价 int64）
	r.LastCommitMessage = info.LastCommitMessage
}

// validateRepoDir 校验 localDir 为存在的绝对路径且为 git 仓库（add/update 共用）。
func validateRepoDir(localDir string) error {
	if !filepath.IsAbs(localDir) {
		return errors.New("目录不是有效的 git 仓库")
	}
	if info, err := os.Stat(localDir); err != nil || !info.IsDir() {
		return errors.New("目录不是有效的 git 仓库")
	}
	if !gitutil.IsRepo(localDir) {
		return errors.New("目录不是有效的 git 仓库")
	}
	return nil
}

// normalizeSubDirs 归一化并校验子目录列表（复刻原 Rust normalize_sub_dir_list）：
//   - 每项 subDir 去首尾空白与路径分隔符后须为存在的目录，任一失败返回「子目录路径无效或不存在」；
//   - subDir 为空的项跳过（防御）；
//   - 每项 subDirDescription 截断到 200 字。
func normalizeSubDirs(dir string, raw []types.RepoSubDir) ([]types.RepoSubDir, error) {
	out := make([]types.RepoSubDir, 0, len(raw))
	for _, item := range raw {
		sub := strings.Trim(strings.TrimSpace(item.SubDir), "/\\")
		if sub == "" {
			continue
		}
		if info, err := os.Stat(filepath.Join(dir, sub)); err != nil || !info.IsDir() {
			return nil, errors.New("子目录路径无效或不存在")
		}
		out = append(out, types.RepoSubDir{SubDir: sub, SubDirDescription: capDescription(item.SubDirDescription)})
	}
	return out, nil
}

// capDescription 截断到最多 200 个字符（按 rune 计）并去首尾空白（仓库描述与子目录描述共用）。
func capDescription(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if rs := []rune(trimmed); len(rs) > 200 {
		return string(rs[:200])
	}
	return trimmed
}
