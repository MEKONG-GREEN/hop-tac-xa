// Ma trận quyền App HTX (contracts/htxroles/htx-roles.v1.json, FN-05) -> bản TS của web và bản Dart của app di động
// (AH-303). Image web build với context apps/web-htx nên web không import được tệp ở gốc repo; bản sinh nằm trong
// src/api/generated. App di động đọc hằng chuỗi JSON trong lib/domain/generated lúc chạy.
import { readFileSync, writeFileSync, existsSync } from "node:fs";
import { join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { repository } from "./gen-module-menus.mjs";

export const SOURCE = "contracts/htxroles/htx-roles.v1.json";
export const TARGET = "apps/web-htx/src/api/generated/htx-roles.ts";
export const DART_TARGET = "apps/htx-app/lib/domain/generated/htx_roles.dart";

const HEADER = [
  `// SINH TỪ ${SOURCE} — không sửa tay; chạy \`npm run gen:api\` trong apps/web-htx.`,
  "// Ma trận quyền Mục V (FN-05): docs/conventions/vai-tro-app-htx.md.",
];

export function renderRoleMatrix(matrix) {
  return [...HEADER, `export const HTX_ROLE_MATRIX = ${JSON.stringify(matrix, null, 2)} as const;`, ""].join("\n");
}

// Chuỗi Dart nhiều dòng '''…''': thoát \ và $ (nội suy); dart format không đổi nội dung chuỗi.
export function renderRoleMatrixDart(matrix) {
  const json = JSON.stringify(matrix, null, 2).replaceAll("\\", "\\\\").replaceAll("$", "\\$");
  if (json.includes("'''")) throw Error(`role matrix contains ''' and cannot be embedded in Dart: ${SOURCE}`);
  return [...HEADER, "const htxRoleMatrixJson = '''", json, "''';", ""].join("\n");
}

export function generateRoleMatrix(root, check = false) {
  const matrix = JSON.parse(readFileSync(join(root, SOURCE), "utf8"));
  for (const [target, render] of [
    [TARGET, renderRoleMatrix],
    [DART_TARGET, renderRoleMatrixDart],
  ]) {
    const file = join(root, target), next = render(matrix);
    const old = existsSync(file) ? readFileSync(file, "utf8").replaceAll("\r\n", "\n") : null;
    if (check && next !== old) throw Error(`role matrix output is stale: ${target}`);
    if (!check && next !== old) writeFileSync(file, next);
  }
}
if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) generateRoleMatrix(repository, process.argv.includes("--check"));
