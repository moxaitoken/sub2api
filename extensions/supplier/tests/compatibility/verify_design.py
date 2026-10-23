"""Validate the six dedicated phase-1 design sections without contacting services."""

import argparse
import json
from pathlib import Path
import re


SECTIONS = (
    "## 1. 表结构与关系图",
    "## 2. 权限矩阵",
    "## 3. 接口定义",
    "## 4. 关键流程及失败处理",
    "## 5. 用量同步算法",
    "## 6. 验收用例",
)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("target", type=Path)
    args = parser.parse_args()
    source = args.target.read_bytes().decode("utf-8")
    headings = source.splitlines()
    present = [heading for heading in SECTIONS if headings.count(heading) == 1]
    indexes = [headings.index(heading) for heading in present]
    valid = len(present) == 6 and indexes == sorted(indexes)
    checks = {
        "dedicated_sections": len(present),
        "er_diagram": "erDiagram" in source,
        "permission_matrix": "| 能力 | 未登录 | 当前供应商登录账号 |" in source,
        "single_login_identity": "one_login_identity" in source and "supplier_id唯一" in source,
        "removed_management": "`/members`" in source and "404" in source and "`/ops`" in source,
        "api_definitions": "/api/supplier/v1" in source,
        "failure_handling": "UPSTREAM_RESULT_UNKNOWN" in source,
        "sync_algorithm": "LIMIT/OFFSET" in source and "reconciled" in source,
        "acceptance_cases": len(re.findall(r"^\| A\d\d \|", source, re.M)),
        "reference_schema": "phase-1-schema.sql" in source,
    }
    valid = valid and all(checks[name] for name in (
        "er_diagram", "permission_matrix", "api_definitions", "failure_handling",
        "sync_algorithm", "reference_schema", "single_login_identity", "removed_management")) and checks["acceptance_cases"] >= 22
    print(json.dumps({"checks": checks, "valid": valid}, ensure_ascii=False, sort_keys=True))
    return 0 if valid else 1


if __name__ == "__main__":
    raise SystemExit(main())
