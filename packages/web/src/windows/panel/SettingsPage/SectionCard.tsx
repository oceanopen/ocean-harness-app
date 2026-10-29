import { Box, Typography } from '@mui/material';

/**
 * 设置页分区卡片公共形态：SectionCard 圆角边框容器 + SectionHeader 灰底标题栏，
 * 设置页各分区页（终端配置/数据清理/应用设置）统一消费。
 */

// 分区卡片标题栏：图标 + 加粗标题（灰底横条 + 下边框）。
export function SectionHeader({ icon, label }: { icon: React.ReactNode; label: string }) {
  return (
    <Box
      sx={{
        display: 'flex',
        alignItems: 'center',
        gap: '12px',
        px: '16px',
        py: '12px',
        bgcolor: 'action.hover',
        borderBottom: '1px solid',
        borderColor: 'divider',
      }}
    >
      {icon}
      <Typography variant="body2" sx={{ fontWeight: 600 }}>
        {label}
      </Typography>
    </Box>
  );
}

// 分区卡片容器：标题栏 + 内容。
export function SectionCard({ header, children }: { header: React.ReactNode; children: React.ReactNode }) {
  return (
    <Box sx={{ borderRadius: '8px', border: '1px solid', borderColor: 'divider', overflow: 'hidden' }}>
      {header}
      {children}
    </Box>
  );
}
