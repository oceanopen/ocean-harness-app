// 文件树图标映射（纯数据）：按扩展名映射 vscode-icons 彩色图标，经 unplugin-icons 的
// ~icons/ 虚拟模块引入（构建期 tree-shake，仅打进用到的图标；Halo ui 同款链路，接入见
// vite.config.ts）。立场与 viewerKind.ts 一致：未命中一律回落 DefaultFileIcon，刻意不做
// 近似映射（没有准确图标就通用文件）。目录图标开合两态由 FileTree 按展开态取用。
// 呈形为模块级 record 而非函数：消费方渲染期做纯引用查找（无函数调用产出组件，
// react/static-components 规则要求的形态）。
import DefaultFile from '~icons/vscode-icons/default-file';
import DefaultFolder from '~icons/vscode-icons/default-folder';
import DefaultFolderOpened from '~icons/vscode-icons/default-folder-opened';
import FileTypeBat from '~icons/vscode-icons/file-type-bat';
import FileTypeC from '~icons/vscode-icons/file-type-c';
import FileTypeCpp from '~icons/vscode-icons/file-type-cpp';
import FileTypeCss from '~icons/vscode-icons/file-type-css';
import FileTypeDart from '~icons/vscode-icons/file-type-dartlang';
import FileTypeDotenv from '~icons/vscode-icons/file-type-dotenv';
import FileTypeGit from '~icons/vscode-icons/file-type-git';
import FileTypeGo from '~icons/vscode-icons/file-type-go';
import FileTypeHtml from '~icons/vscode-icons/file-type-html';
import FileTypeImage from '~icons/vscode-icons/file-type-image';
import FileTypeJava from '~icons/vscode-icons/file-type-java';
import FileTypeJs from '~icons/vscode-icons/file-type-js';
import FileTypeJson from '~icons/vscode-icons/file-type-json';
import FileTypeKotlin from '~icons/vscode-icons/file-type-kotlin';
import FileTypeLess from '~icons/vscode-icons/file-type-less';
import FileTypeLua from '~icons/vscode-icons/file-type-lua';
import FileTypeMarkdown from '~icons/vscode-icons/file-type-markdown';
import FileTypePhp from '~icons/vscode-icons/file-type-php';
import FileTypePowershell from '~icons/vscode-icons/file-type-powershell';
import FileTypePython from '~icons/vscode-icons/file-type-python';
import FileTypeReactjs from '~icons/vscode-icons/file-type-reactjs';
import FileTypeReactts from '~icons/vscode-icons/file-type-reactts';
import FileTypeRuby from '~icons/vscode-icons/file-type-ruby';
import FileTypeRust from '~icons/vscode-icons/file-type-rust';
import FileTypeSass from '~icons/vscode-icons/file-type-sass';
import FileTypeShell from '~icons/vscode-icons/file-type-shell';
import FileTypeSql from '~icons/vscode-icons/file-type-sql';
import FileTypeSvelte from '~icons/vscode-icons/file-type-svelte';
import FileTypeSvg from '~icons/vscode-icons/file-type-svg';
import FileTypeSwift from '~icons/vscode-icons/file-type-swift';
import FileTypeText from '~icons/vscode-icons/file-type-text';
import FileTypeToml from '~icons/vscode-icons/file-type-toml';
import FileTypeTypescript from '~icons/vscode-icons/file-type-typescript';
import FileTypeVue from '~icons/vscode-icons/file-type-vue';
import FileTypeWebp from '~icons/vscode-icons/file-type-webp';
import FileTypeXml from '~icons/vscode-icons/file-type-xml';
import FileTypeYaml from '~icons/vscode-icons/file-type-yaml';
import FileTypeZip from '~icons/vscode-icons/file-type-zip';

export { DefaultFile as DefaultFileIcon, DefaultFolder as FolderClosedIcon, DefaultFolderOpened as FolderOpenedIcon };

export type FileIcon = typeof DefaultFile;

/// 扩展名 → 文件图标。key 兼收文件扩展名（ts/go）与 dotfile 名段（gitignore——
/// .gitignore 的 extOf 即 gitignore）。
export const FILE_ICONS: Readonly<Record<string, FileIcon>> = {
  'js': FileTypeJs,
  'mjs': FileTypeJs,
  'cjs': FileTypeJs,
  'jsx': FileTypeReactjs,
  'ts': FileTypeTypescript,
  'mts': FileTypeTypescript,
  'cts': FileTypeTypescript,
  'tsx': FileTypeReactts,
  'json': FileTypeJson,
  'go': FileTypeGo,
  'py': FileTypePython,
  'pyi': FileTypePython,
  'rs': FileTypeRust,
  'java': FileTypeJava,
  'kt': FileTypeKotlin,
  'kts': FileTypeKotlin,
  'swift': FileTypeSwift,
  'php': FileTypePhp,
  'yml': FileTypeYaml,
  'yaml': FileTypeYaml,
  'toml': FileTypeToml,
  'sql': FileTypeSql,
  'c': FileTypeC,
  'h': FileTypeC,
  'cc': FileTypeCpp,
  'cpp': FileTypeCpp,
  'hpp': FileTypeCpp,
  'c++': FileTypeCpp,
  'css': FileTypeCss,
  'scss': FileTypeSass,
  'sass': FileTypeSass,
  'less': FileTypeLess,
  'html': FileTypeHtml,
  'htm': FileTypeHtml,
  'svelte': FileTypeSvelte,
  'vue': FileTypeVue,
  'xml': FileTypeXml,
  'svg': FileTypeSvg,
  'md': FileTypeMarkdown,
  'markdown': FileTypeMarkdown,
  'txt': FileTypeText,
  'sh': FileTypeShell,
  'bash': FileTypeShell,
  'zsh': FileTypeShell,
  'ps1': FileTypePowershell,
  'bat': FileTypeBat,
  'cmd': FileTypeBat,
  'lua': FileTypeLua,
  'rb': FileTypeRuby,
  'dart': FileTypeDart,
  'env': FileTypeDotenv,
  'gitignore': FileTypeGit,
  'gitattributes': FileTypeGit,
  'gitmodules': FileTypeGit,
  'gitconfig': FileTypeGit,
  'png': FileTypeImage,
  'jpg': FileTypeImage,
  'jpeg': FileTypeImage,
  'gif': FileTypeImage,
  'ico': FileTypeImage,
  'bmp': FileTypeImage,
  'webp': FileTypeWebp,
  'zip': FileTypeZip,
  'tar': FileTypeZip,
  'gz': FileTypeZip,
  'tgz': FileTypeZip,
  'rar': FileTypeZip,
  '7z': FileTypeZip,
};
