; Ocean Harness CLI 注册清理：卸载时移除 shim 目录与用户 PATH 注入段（幂等，缺失即静默跳过）。
; PowerShell 单行负责注册表 PATH 段摘除（SetEnvironmentVariable 自带 Environment 变更广播）；
; NSIS 字符串里 $$ 转义为字面 $，避免被 NSIS 变量展开。

!macro NSIS_HOOK_POSTUNINSTALL
  RMDir /r "$LOCALAPPDATA\OceanHarness\bin"
  RMDir "$LOCALAPPDATA\OceanHarness"
  nsExec::Exec "powershell -NoProfile -ExecutionPolicy Bypass -Command $$p=[Environment]::GetEnvironmentVariable('Path','User'); if ($$p) { $$d=[Environment]::GetFolderPath('LocalApplicationData') + '\OceanHarness\bin'; $$n=($$p -split ';' | Where-Object { $$_ -and $$_.TrimEnd('\').ToLower() -ne $$d.ToLower() }) -join ';'; [Environment]::SetEnvironmentVariable('Path', $$n, 'User') }"
!macroend
