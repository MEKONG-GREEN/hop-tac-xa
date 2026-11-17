// Ma trận quyền App HTX (contracts/htxroles/htx-roles.v1.json, FN-05) -> bản TS của web. Image web build với
// context apps/web-htx nên web không import được tệp ở gốc repo; bản sinh nằm trong src/api/generated.
import { readFileSync, writeFileSync, existsSync } from "node:fs";
import { join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { repository } from "./gen-module-menus.mjs";

export const SOURCE = "contracts/htxroles/htx-roles.v1.json";
export const TARGET = "apps/web-htx/src/api/generated/htx-roles.ts";

export function renderRoleMatrix(matrix) {
  return [
    `// SINH TỪ ${SOURCE} — không sửa tay; chạy \`npm run gen:api\` trong apps/web-htx.`,
    "// Ma trận quyền Mục V (FN-05): docs/conventions/vai-tro-app-htx.md.",
    `export const HTX_ROLE_MATRIX = ${JSON.stringify(matrix, null, 2)} as const;`,
    "",
  ].join("\n");
}

export function generateRoleMatrix(root, check = false) {
  const matrix = JSON.parse(readFileSync(join(root, SOURCE), "utf8"));
  const file = join(root, TARGET), next = renderRoleMatrix(matrix);
  const old = existsSync(file) ? readFileSync(file, "utf8").replaceAll("\r\n", "\n") : null;
  if (check && next !== old) throw Error(`role matrix output is stale: ${TARGET}`);
  if (!check && next !== old) writeFileSync(file, next);
}
if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) generateRoleMatrix(repository, process.argv.includes("--check"));
