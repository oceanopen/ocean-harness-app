-- +goose Up
-- t_local_repositories 加 default_branch：仓库默认分支（origin/HEAD 指向的远程默认分支）。
-- ParseInfo 经 git symbolic-ref refs/remotes/origin/HEAD 解析填充（克隆仓库自动设置；
-- 本地 init 未克隆/无 origin 留空）。供开发工作台 worktree 初始化「基准分支」默认值：
-- 优先 default_branch，缺失回退 current_branch。
ALTER TABLE t_local_repositories ADD COLUMN default_branch TEXT NOT NULL DEFAULT '';
