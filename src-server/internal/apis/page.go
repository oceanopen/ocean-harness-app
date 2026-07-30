package apis

// PageInfo 是分页参数与结果（请求入参 + 响应回填共用）。
//
// 请求 DTO 内嵌本类型（如 IssueGetListByPageRequest{ ...; PageInfo apis.PageInfo }），
// service 用 GetOffset/GetPageSize 取偏移与条数查询、回填 TotalCount 后随 JsonPageOK 返回。
type PageInfo struct {
	PageSize    int   `json:"pageSize"`
	CurrentPage int   `json:"currentPage"`
	TotalCount  int64 `json:"totalCount"`
}

// GetCurrentPage 返回当前页（≤0 默认 1）。
func (p *PageInfo) GetCurrentPage() int {
	if p.CurrentPage <= 0 {
		p.CurrentPage = 1
	}
	return p.CurrentPage
}

// GetPageSize 返回每页条数（≤0 默认 10）。
func (p *PageInfo) GetPageSize() int {
	if p.PageSize <= 0 {
		p.PageSize = 10
	}
	return p.PageSize
}

// GetOffset 返回 LIMIT/OFFSET 的 offset：先经 GetCurrentPage/GetPageSize 走默认值，避免负 offset。
func (p *PageInfo) GetOffset() int {
	return (p.GetCurrentPage() - 1) * p.GetPageSize()
}

// PageData 是分页接口的 data 载荷（{list, pageInfo}），由 JsonPageOK 包裹。
type PageData struct {
	List     interface{} `json:"list"`
	PageInfo *PageInfo   `json:"pageInfo"`
}
