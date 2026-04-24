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
OUT = ROOT / "internal" / "modules" / "templatecenter" / "generated_seed_definitions.json"

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
                "examples": [],
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


def main() -> None:
    definitions = []
    for key, path in DOCS.items():
        definitions.extend(parse_doc(key, path))
    OUT.write_text(json.dumps(definitions, ensure_ascii=False, indent=2))
    print(f"generated {len(definitions)} templates -> {OUT}")


if __name__ == "__main__":
    main()
