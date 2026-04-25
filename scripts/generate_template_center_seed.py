#!/usr/bin/env python3
from __future__ import annotations

import json
import re
import unicodedata
from dataclasses import dataclass
from pathlib import Path
from typing import Any

ROOT = Path(__file__).resolve().parents[1]
DOCS = {
    "model": ROOT.parent / "docs" / "agent_ecommerce_prompt_模特图系列.md",
    "product": ROOT.parent / "docs" / "agent_ecommerce_prompt商品图&套图系列.md",
}
EXAMPLES_ROOT = ROOT.parent / "infra" / "examples"
OUT = ROOT / "internal" / "modules" / "templatecenter" / "generated_seed_definitions.json"
EXAMPLE_MANIFEST_OUT = ROOT / "internal" / "modules" / "templatecenter" / "example_asset_manifest.json"

@dataclass
class SectionMeta:
    doc_key: str
    series: str
    capability_type: str
    interaction_mode: str
    modality: str
    executor_type: str
    tool_slug: str
    route: str
    platform_tags: list[str]
    industry_tags: list[str]
    scenario_tags: list[str]
    input_fields: list[dict[str, Any]]
    default_ratio: str
    suite_mode: str | None = None

SECTION_META: dict[str, SectionMeta] = {
    "1.1": SectionMeta("model", "model_image", "model_swap", "upload_form", "image", "image_tool", "changing-model", "/draw/changing-model", ["amazon", "tiktok-shop", "independent"], ["fashion", "apparel"], ["real-model", "market-localization"], [{"key": "garment_image", "type": "image", "required": True, "role": "primary_garment_asset"}, {"key": "target_market", "type": "select", "required": False, "options": ["amazon-us", "amazon-eu", "tiktok-shop-us", "independent"]}], "4:5"),
    "1.2": SectionMeta("model", "model_image", "mannequin_to_model", "upload_form", "image", "image_tool", "changing-mannequin", "/draw/changing-mannequin", ["amazon", "walmart", "independent"], ["fashion", "apparel"], ["ghost-mannequin", "on-model"], [{"key": "mannequin_image", "type": "image", "required": True, "role": "mannequin_source_asset"}, {"key": "style_direction", "type": "text", "required": False}], "4:5"),
    "1.3": SectionMeta("model", "model_image", "background_replace", "upload_form", "image", "image_tool", "changing-bg", "/draw/changing-bg", ["amazon", "tiktok-shop", "independent"], ["fashion", "apparel"], ["background", "scene"], [{"key": "model_image", "type": "image", "required": True, "role": "model_source_asset"}, {"key": "background_direction", "type": "text", "required": False}], "4:5"),
    "1.4": SectionMeta("model", "model_image", "virtual_tryon", "upload_form", "image", "image_tool", "ai-dressing", "/draw/ai-dressing", ["amazon", "tiktok-shop", "independent"], ["fashion", "apparel"], ["try-on", "fit"], [{"key": "garment_image", "type": "image", "required": True, "role": "garment_asset"}, {"key": "base_model_image", "type": "image", "required": False, "role": "base_model_asset"}], "4:5"),
    "1.5": SectionMeta("model", "model_image", "accessory_on_model", "upload_form", "image", "image_tool", "ai-wearable", "/draw/ai-wearable", ["amazon", "independent"], ["accessories", "jewelry", "wearable"], ["detail", "on-model"], [{"key": "accessory_image", "type": "image", "required": True, "role": "accessory_asset"}, {"key": "wear_mode", "type": "text", "required": False}], "1:1"),
    "1.6": SectionMeta("model", "model_image", "pose_variation", "upload_form", "image", "image_tool", "ai-posture", "/draw/ai-posture", ["amazon", "tiktok-shop", "independent"], ["fashion", "apparel"], ["pose", "variation"], [{"key": "model_image", "type": "image", "required": True, "role": "base_model_asset"}, {"key": "pose_direction", "type": "text", "required": False}], "4:5"),
    "2.1": SectionMeta("product", "product_image", "product_scene_compositing", "upload_form", "image", "image_tool", "ai-product", "/draw/ai-product", ["amazon", "tiktok-shop", "independent"], ["consumer-goods", "beauty", "home"], ["scene", "hero-visual"], [{"key": "product_image", "type": "image", "required": True, "role": "primary_product_asset"}, {"key": "scene_reference_image", "type": "image", "required": False, "role": "reference_scene_asset"}], "1:1"),
    "2.2": SectionMeta("product", "product_image", "product_swap", "upload_form", "image", "image_tool", "product-replacement", "/draw/product-replacement", ["amazon", "walmart", "independent"], ["consumer-goods", "electronics", "home"], ["replace", "sku-variation"], [{"key": "reference_scene_image", "type": "image", "required": True, "role": "reference_scene_asset"}, {"key": "replacement_product_image", "type": "image", "required": True, "role": "replacement_product_asset"}], "1:1"),
    "2.3": SectionMeta("product", "product_image", "scene_multiplication", "upload_form", "image", "image_tool", "image-fission", "/draw/image-fission", ["amazon", "tiktok-shop", "independent"], ["consumer-goods", "generic"], ["scene", "batch"], [{"key": "reference_scene_image", "type": "image", "required": True, "role": "reference_scene_asset"}, {"key": "variation_goal", "type": "text", "required": False}], "1:1"),
    "2.4": SectionMeta("product", "product_image", "scene_asset_generation", "form_based", "image", "image_tool", "scene-image", "/draw/scene-image", ["amazon", "tiktok-shop", "independent"], ["generic", "background-asset"], ["scene-asset", "text-to-image"], [{"key": "scene_prompt", "type": "textarea", "required": True}, {"key": "style_reference", "type": "image", "required": False, "role": "style_reference_asset"}], "1:1"),
    "2.5": SectionMeta("product", "product_image", "hand_hold_product", "upload_form", "image", "image_tool", "handheld-goods", "/draw/handheld-goods", ["amazon", "tiktok-shop", "independent"], ["consumer-goods", "beauty", "electronics"], ["handheld", "on-hand"], [{"key": "product_image", "type": "image", "required": True, "role": "product_asset"}, {"key": "hand_reference", "type": "image", "required": False, "role": "hand_reference_asset"}], "1:1"),
    "2.6": SectionMeta("product", "product_image", "product_retouch", "upload_form", "image", "image_tool", "product-refine", "/draw/product-refine", ["amazon", "walmart", "independent"], ["consumer-goods", "electronics", "beauty"], ["retouch", "enhancement"], [{"key": "product_image", "type": "image", "required": True, "role": "product_asset"}, {"key": "retouch_goal", "type": "text", "required": False}], "1:1"),
    "3.1": SectionMeta("product", "workflow_suite", "clothing_photo_package", "wizard_based", "workflow", "hybrid_workflow", "clothing-image-suite", "/draw/clothing-image-suite", ["amazon", "tiktok-shop", "independent"], ["fashion", "apparel"], ["suite", "listing-package"], [{"key": "garment_images", "type": "image[]", "required": True, "role": "garment_asset_batch"}, {"key": "brand_style_reference", "type": "image", "required": False, "role": "brand_reference_asset"}], "4:5", "clothing_suite"),
    "3.2": SectionMeta("product", "workflow_suite", "product_photo_package", "wizard_based", "workflow", "hybrid_workflow", "product-image-suite", "/draw/product-image-suite", ["amazon", "walmart", "independent"], ["consumer-goods", "electronics", "beauty"], ["suite", "listing-package"], [{"key": "product_images", "type": "image[]", "required": True, "role": "product_asset_batch"}, {"key": "brand_style_reference", "type": "image", "required": False, "role": "brand_reference_asset"}], "1:1", "product_suite"),
}

SECTION_RE = re.compile(r"^###\s+([123]\.[0-9]+)\s+(.+)$")
TABLE_ROW_RE = re.compile(r"^\|\s*([^|]+?)\s*\|\s*([^|]+?)\s*\|\s*([^|]+?)\s*\|\s*(.+?)\s*\|\s*$")

TOOL_EXAMPLE_DIRS: dict[str, list[str]] = {
    "changing-model": ["模特图系列/真人换模特", "Model/ModelSwap"],
    "changing-mannequin": ["模特图系列/人台换模特", "Model/Mannequin"],
    "changing-bg": ["模特图系列/换背景", "Model/BackgroundReplace"],
    "ai-dressing": ["模特图系列/AI穿衣", "Model/VirtualTryOn"],
    "ai-wearable": ["模特图系列/穿戴商品", "Model/AccessoriesOnModel"],
    "ai-posture": ["模特图系列/姿势裂变"],
    "ai-product": ["商品图系列/商品场景合成"],
    "product-replacement": ["商品图系列/商品替换"],
    "image-fission": ["商品图系列/场景裂变"],
    "scene-image": ["商品图系列/场景素材生成"],
    "handheld-goods": ["商品图系列/手持商品"],
    "product-refine": ["商品图系列/商品精修"],
    "clothing-image-suite": ["套图系列/服装套图"],
    "product-image-suite": ["套图系列/商品套图"],
}


def slugify(value: str) -> str:
    value = unicodedata.normalize("NFKD", value)
    value = value.encode("ascii", "ignore").decode("ascii")
    value = re.sub(r"[^a-zA-Z0-9]+", "-", value).strip("-").lower()
    return value or "template"


def infer_count(text: str, fallback: int) -> int:
    for pattern in [r"(\d+)张", r"(\d+)个", r"Generate\s+(\d+)", r"generate\s+(\d+)", r"(\d+)-image"]:
        m = re.search(pattern, text)
        if m:
            return int(m.group(1))
    return fallback


def infer_ratio(text: str, fallback: str) -> str:
    m = re.search(r"(1:1|4:5|3:4|9:16|16:9)", text)
    return m.group(1) if m else fallback


def normalize_example_name(value: str) -> str:
    value = unicodedata.normalize("NFKC", value)
    value = value.lower()
    value = re.sub(r"\.(jpg|jpeg|png|webp)$", "", value)
    value = re.sub(r"^(模特图系列|商品图系列|套图系列)-", "", value)
    value = re.sub(
        r"^(真人换模特|人台换模特|换背景|ai穿衣|穿戴商品|姿势裂变|商品场景合成|商品替换|场景裂变|场景素材生成|手持商品|商品精修|服装套图|商品套图)-",
        "",
        value,
    )
    value = value.replace("（", "(").replace("）", ")")
    value = re.sub(r"[\s_\-/]+", "", value)
    value = re.sub(r"[()（）,.，:：'\"+]+", "", value)
    return value


def build_example_index() -> dict[str, list[dict[str, str]]]:
    index: dict[str, list[dict[str, str]]] = {}
    if not EXAMPLES_ROOT.exists():
        return index
    for path in sorted(EXAMPLES_ROOT.rglob("*")):
        if path.suffix.lower() not in {".jpg", ".jpeg", ".png", ".webp"}:
            continue
        rel_path = path.relative_to(ROOT.parent).as_posix()
        rel_dir = path.parent.relative_to(EXAMPLES_ROOT).as_posix()
        normalized = normalize_example_name(path.stem)
        index.setdefault(rel_dir, []).append({
            "normalized_name": normalized,
            "title": path.stem,
            "asset_ref": rel_path,
            "dir": rel_dir,
        })
    return index


EXAMPLE_INDEX = build_example_index()


def build_example_source_ref(tool_slug: str, template_id: str, example_index: int) -> str:
    return f"templates/{tool_slug}/{template_id}/example-{example_index}"


def build_storage_file_name(tool_slug: str, template_id: str, example_index: int, asset_ref: str) -> str:
    suffix = Path(asset_ref).suffix.lower() or ".png"
    return f"{tool_slug}/{template_id.lower()}-example-{example_index}{suffix}"


def match_examples(meta: SectionMeta, template_id: str, template_name: str, zh_desc: str) -> list[dict[str, Any]]:
    candidates: list[dict[str, Any]] = []
    normalized_template_name = normalize_example_name(template_name)
    normalized_desc = normalize_example_name(zh_desc)
    for rel_dir in TOOL_EXAMPLE_DIRS.get(meta.tool_slug, []):
        for item in EXAMPLE_INDEX.get(rel_dir, []):
            candidate_name = item["normalized_name"]
            score = 0
            if candidate_name == normalized_template_name:
                score = 100
            elif normalized_template_name and normalized_template_name in candidate_name:
                score = 80
            elif candidate_name and candidate_name in normalized_template_name:
                score = 75
            elif normalized_desc and candidate_name and candidate_name in normalized_desc:
                score = 60
            if score <= 0:
                continue
            candidates.append({
                "score": score,
                "title": item["title"],
                "asset_ref": item["asset_ref"],
                "dir": item["dir"],
            })
    if not candidates:
        return []
    candidates.sort(key=lambda item: (-item["score"], item["asset_ref"]))
    best = candidates[0]
    example_index = 1
    source_ref = build_example_source_ref(meta.tool_slug, template_id, example_index)
    example = {
        "id": f"{template_id.lower().replace('-', '_')}_example_{example_index}",
        "exampleType": "reference_image",
        "title": best["title"],
        "description": f"Matched from infra/examples/{best['dir']}",
        "assetRef": best["asset_ref"],
        "sourceRef": source_ref,
        "storageFileName": build_storage_file_name(meta.tool_slug, template_id, example_index, best["asset_ref"]),
    }
    return [example]


def parse_doc(doc_key: str, path: Path) -> list[dict[str, Any]]:
    text = path.read_text()
    lines = text.splitlines()
    results: list[dict[str, Any]] = []
    idx = 0
    section_order = 0
    while idx < len(lines):
        match = SECTION_RE.match(lines[idx])
        if not match:
            idx += 1
            continue
        section_id = match.group(1)
        if section_id not in SECTION_META:
            idx += 1
            continue
        meta = SECTION_META[section_id]
        section_title = match.group(2)
        section_order += 1

        j = idx + 1
        code_block = ""
        while j < len(lines) and not lines[j].startswith("| 模板ID"):
            if lines[j].startswith("```"):
                k = j + 1
                buf = []
                while k < len(lines) and not lines[k].startswith("```"):
                    buf.append(lines[k])
                    k += 1
                code_block = "\n".join(buf).strip()
                j = k
                break
            j += 1
        while j < len(lines) and not lines[j].startswith("| 模板ID"):
            j += 1
        if j >= len(lines):
            idx += 1
            continue
        row_index = 0
        row_ptr = j + 2
        while row_ptr < len(lines):
            row = lines[row_ptr]
            if not row.startswith("|"):
                break
            if set(row.replace("|", "").replace("-", "").replace(" ", "")) == set():
                row_ptr += 1
                continue
            row_match = TABLE_ROW_RE.match(row)
            if not row_match:
                row_ptr += 1
                continue
            row_index += 1
            template_id = row_match.group(1).strip()
            template_name = row_match.group(2).strip()
            zh_desc = row_match.group(3).strip()
            en_prompt = row_match.group(4).strip()
            recommend_score = max(100, 10000 - section_order * 100 - row_index)
            featured = row_index <= 2
            ratio = infer_ratio(zh_desc + " " + en_prompt, meta.default_ratio)
            count = infer_count(zh_desc + " " + en_prompt, 4 if meta.modality == "image" else 1)
            source_asset_ref = f"docs/{path.name}#{template_id}"
            asset_requirements = []
            for field in meta.input_fields:
                item = {
                    "slot": field["key"],
                    "label": field["key"],
                    "required": bool(field.get("required", False)),
                }
                if "type" in field:
                    item["fieldType"] = field["type"]
                if field.get("type", "").startswith("image"):
                    item["acceptedTypes"] = ["jpg", "png", "webp"]
                asset_requirements.append(item)
            input_schema = {"mode": meta.interaction_mode, "fields": meta.input_fields}
            if meta.modality == "workflow":
                output_schema = {
                    "primaryOutput": "workflow",
                    "workflow": {
                        "count": count,
                        "suiteMode": meta.suite_mode,
                        "outputs": ["hero", "detail", "scene", "marketing"],
                    },
                }
            else:
                output_schema = {
                    "primaryOutput": meta.modality,
                    "image": {"count": count, "ratio": ratio},
                }
            execution_schema = {
                "route": meta.route,
                "supportsAsyncJob": meta.executor_type != "chat",
                "supportsBatch": meta.modality == "workflow" or any(x in zh_desc for x in ["批量", "套图", "裂变"]),
            }
            prompt_layers = {
                "l1": {"name": "tool_system_prompt", "content": code_block},
                "l2": {"name": "template_case_prompt", "content": en_prompt},
                "l3": {"name": "user_diy_prompt", "defaultContent": zh_desc, "editable": True},
            }
            default_variables = {
                "templateID": template_id,
                "templateName": template_name,
                "sectionID": section_id,
                "sectionTitle": section_title,
                "englishPrompt": en_prompt,
                "chineseDescription": zh_desc,
                "assetRequirements": asset_requirements,
            }
            examples = match_examples(meta, template_id, template_name, zh_desc)
            if examples:
                default_variables["exampleAssetRefs"] = [item["assetRef"] for item in examples]
                default_variables["exampleSourceRefs"] = [item["sourceRef"] for item in examples]
            results.append({
                "id": f"tpl_{template_id.lower().replace('-', '_')}",
                "externalCode": template_id,
                "slug": f"{meta.tool_slug}-{template_id.lower()}-{slugify(template_name)[:32]}",
                "modality": meta.modality,
                "executorType": meta.executor_type,
                "series": meta.series,
                "capabilityType": meta.capability_type,
                "interactionMode": meta.interaction_mode,
                "featured": featured,
                "recommendScore": recommend_score,
                "sourceAssetRef": source_asset_ref,
                "platformTags": meta.platform_tags,
                "industryTags": meta.industry_tags,
                "scenarioTags": meta.scenario_tags + [section_id, template_id.lower()],
                "executionSchema": execution_schema,
                "toolBinding": {"toolSlug": meta.tool_slug},
                "inputSchema": input_schema,
                "outputSchema": output_schema,
                "promptLayers": prompt_layers,
                "defaultVariables": default_variables,
                "examples": examples,
                "localeZH": {
                    "name": template_name,
                    "summary": zh_desc,
                    "description": zh_desc,
                    "inputDescription": "需准备: " + " / ".join([f["key"] for f in meta.input_fields]),
                    "outputDescription": f"输出类型: {meta.modality}；默认 {count} 项；比例 {ratio}",
                },
                "localeEN": {
                    "name": template_name,
                    "summary": en_prompt,
                    "description": en_prompt,
                    "inputDescription": "Inputs: " + " / ".join([f["key"] for f in meta.input_fields]),
                    "outputDescription": f"Output: {meta.modality}; default {count} items; ratio {ratio}",
                },
            })
            row_ptr += 1
        idx = row_ptr
    return results


def build_example_manifest(definitions: list[dict[str, Any]]) -> dict[str, Any]:
    items: list[dict[str, Any]] = []
    seen_source_refs: set[str] = set()
    for definition in definitions:
        tool_slug = definition.get("toolBinding", {}).get("toolSlug", "")
        template_code = definition.get("externalCode", "")
        template_name = definition.get("localeZH", {}).get("name", "")
        for example in definition.get("examples", []):
            source_ref = example.get("sourceRef")
            asset_ref = example.get("assetRef")
            if not isinstance(source_ref, str) or not source_ref or source_ref in seen_source_refs:
                continue
            if not isinstance(asset_ref, str) or not asset_ref:
                continue
            seen_source_refs.add(source_ref)
            items.append({
                "productCode": "ecommerce",
                "category": "template-examples",
                "sourceType": "template_example",
                "sourceRef": source_ref,
                "assetRef": asset_ref,
                "storageFileName": example.get("storageFileName") or build_storage_file_name(tool_slug, template_code, 1, asset_ref),
                "title": example.get("title") or template_name,
                "description": example.get("description") or f"Template example for {template_code}",
                "tags": ["template-example", tool_slug, template_code],
                "metadata": {
                    "templateCode": template_code,
                    "templateName": template_name,
                    "toolSlug": tool_slug,
                    "exampleId": example.get("id"),
                    "assetRef": asset_ref,
                },
            })
    return {
        "version": 1,
        "items": items,
    }


def main() -> None:
    definitions = []
    for key, path in DOCS.items():
        definitions.extend(parse_doc(key, path))
    OUT.write_text(json.dumps(definitions, ensure_ascii=False, indent=2))
    EXAMPLE_MANIFEST_OUT.write_text(json.dumps(build_example_manifest(definitions), ensure_ascii=False, indent=2))
    print(f"generated {len(definitions)} templates -> {OUT}")
    print(f"generated template example manifest -> {EXAMPLE_MANIFEST_OUT}")


if __name__ == "__main__":
    main()
