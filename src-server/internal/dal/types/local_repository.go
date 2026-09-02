package types

import (
	"encoding/json"
	"time"

	"ocean-harness/src-server/internal/dal/model"
)

// RepoSubDir 是仓库下的项目子目录（monorepo 多 package 场景）。字段对齐前端 RepoSubDir。
type RepoSubDir struct {
	SubDir            string `json:"subDir"`            // 相对仓库根目录的路径
	SubDirDescription string `json:"subDirDescription"` // 子目录描述
}

// 每 action 一个独立 Request 类型（对齐项目既有 DTO 风格）。
// local_dir 必须为存在的绝对路径且为 git 仓库（service 层校验，失败返回哨兵字符串）。

// LocalRepositoryGetListRequest 是 POST /api/localRepository/getList 的入参（无筛选，列全部）。
type LocalRepositoryGetListRequest struct{}

// LocalRepositoryGetInfoRequest 是 POST /api/localRepository/getInfo 的入参。
type LocalRepositoryGetInfoRequest struct {
	ID int `json:"id" binding:"required"`
}

// LocalRepositoryCreateRequest 是 POST /api/localRepository/create 的入参。
// remoteUrl/currentBranch/lastCommitAt/lastCommitMessage 不入参（service 调 gitutil 解析写入）。
type LocalRepositoryCreateRequest struct {
	Name        string       `json:"name" binding:"required,max=100"`
	LocalDir    string       `json:"localDir" binding:"required"`
	Description string       `json:"description" binding:"omitempty"`
	SubDirList  []RepoSubDir `json:"subDirList"`
}

// LocalRepositoryUpdateRequest 是 POST /api/localRepository/update 的入参。
type LocalRepositoryUpdateRequest struct {
	ID          int          `json:"id" binding:"required"`
	Name        string       `json:"name" binding:"required,max=100"`
	LocalDir    string       `json:"localDir" binding:"required"`
	Description string       `json:"description" binding:"omitempty"`
	SubDirList  []RepoSubDir `json:"subDirList"`
}

// LocalRepositoryDeleteRequest 是 POST /api/localRepository/delete 的入参（物理删除，释放 local_dir）。
type LocalRepositoryDeleteRequest struct {
	ID int `json:"id" binding:"required"`
}

// LocalRepositoryRefreshRequest 是 POST /api/localRepository/refresh 的入参（重解析单个仓库 git 信息）。
type LocalRepositoryRefreshRequest struct {
	ID int `json:"id" binding:"required"`
}

// LocalRepositoryGetLocalBranchesRequest 是 POST /api/localRepository/getLocalBranches 的入参（列仓库本地分支）。
// 仅本地分支（git branch）；远程分支能力后续按需扩展（getRemoteBranches）。
type LocalRepositoryGetLocalBranchesRequest struct {
	ID int `json:"id" binding:"required"`
}

// LocalRepositoryResponseData 是仓库响应：DO 字段平铺 + subDirList 由 JSON 文本反序列化为数组。
// 不嵌入 *model.LocalRepository（其 SubDirList 为 string），改为扁平结构由 FromModel 装配，
// 与 issue 响应「service 层组装呈现形态」的思路一致。
type LocalRepositoryResponseData struct {
	ID                int          `json:"id"`
	Name              string       `json:"name"`
	LocalDir          string       `json:"localDir"`
	Description       string       `json:"description"`
	SubDirList        []RepoSubDir `json:"subDirList"`
	RemoteURL         string       `json:"remoteUrl"`
	CurrentBranch     string       `json:"currentBranch"`
	DefaultBranch     string       `json:"defaultBranch"`
	LastCommitAt      int          `json:"lastCommitAt"`
	LastCommitMessage string       `json:"lastCommitMessage"`
	CreatedAt         time.Time    `json:"createdAt"`
	UpdatedAt         time.Time    `json:"updatedAt"`
}

// FromModel 把 DO 转为响应：sub_dir_list JSON 文本 → 数组，解析失败兜底空数组（不阻塞列表加载）。
func (LocalRepositoryResponseData) FromModel(r *model.LocalRepository) LocalRepositoryResponseData {
	out := LocalRepositoryResponseData{
		ID:                r.ID,
		Name:              r.Name,
		LocalDir:          r.LocalDir,
		Description:       r.Description,
		SubDirList:        []RepoSubDir{},
		RemoteURL:         r.RemoteURL,
		CurrentBranch:     r.CurrentBranch,
		DefaultBranch:     r.DefaultBranch,
		LastCommitAt:      r.LastCommitAt,
		LastCommitMessage: r.LastCommitMessage,
		CreatedAt:         r.CreatedAt,
		UpdatedAt:         r.UpdatedAt,
	}
	if r.SubDirList != "" {
		_ = json.Unmarshal([]byte(r.SubDirList), &out.SubDirList) // 失败保留空数组
	}
	if out.SubDirList == nil {
		out.SubDirList = []RepoSubDir{}
	}
	return out
}
