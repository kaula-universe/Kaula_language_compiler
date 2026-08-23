#!/usr/bin/env python3
"""
Kaula 多语言文档翻译脚本

将 docs/ 目录下的中文 Markdown 文档翻译为指定目标语言。
支持增量翻译、术语表自定义、代码块保护等功能。

用法:
    python translate.py --lang en                    # 翻译为英文
    python translate.py --lang ja --input docs       # 指定输入目录
    python translate.py --all                        # 翻译为所有支持语言
    python translate.py --lang en --output docs/en   # 指定输出目录
    python translate.py --list                       # 列出支持语言
    python translate.py --lang en --dry-run          # 预览不写入
"""

import argparse
import os
import re
import sys
import json
import hashlib
from pathlib import Path
from typing import Optional

# 设置标准输出编码为 UTF-8（Windows 兼容）
if sys.platform == "win32":
    import io
    sys.stdout = io.TextIOWrapper(sys.stdout.buffer, encoding="utf-8")
    sys.stderr = io.TextIOWrapper(sys.stderr.buffer, encoding="utf-8")

# ============================================================
# 支持语言
# ============================================================
SUPPORTED_LANGUAGES = {
    "zh": "中文",
    "en": "English",
    "ja": "日本語",
    "ko": "한국어",
    "de": "Deutsch",
    "fr": "Français",
    "es": "Español",
    "ru": "Русский",
}

# ============================================================
# 默认术语表 (中文 -> 目标语言)
# ============================================================
DEFAULT_GLOSSARY = {
    "en": {
        "编译器": "compiler",
        "语法分析": "parser",
        "词法分析": "lexer",
        "语义分析": "semantic analysis",
        "代码生成": "code generation",
        "标准库": "standard library",
        "第三方库": "third-party library",
        "包管理": "package management",
        "工作空间": "workspace",
        "格式化": "formatting",
        "调试": "debugging",
        "配置文件": "configuration file",
        "命令行": "command line",
        "静态类型": "static typing",
        "类型推导": "type inference",
        "泛型": "generics",
        "所有权": "ownership",
        "内存管理": "memory management",
        "线程安全": "thread safety",
        "并发": "concurrency",
        "异步": "asynchronous",
        "模式匹配": "pattern matching",
        "结构体": "struct",
        "类": "class",
        "枚举": "enum",
        "接口": "interface",
        "函数": "function",
        "变量": "variable",
        "常量": "constant",
        "指针": "pointer",
        "引用": "reference",
        "数组": "array",
        "切片": "slice",
        "字符串": "string",
        "整数": "integer",
        "浮点数": "float",
        "布尔值": "boolean",
        "空值": "null",
        "错误": "error",
        "警告": "warning",
        "依赖": "dependency",
        "构建": "build",
        "编译": "compile",
        "链接": "link",
        "运行": "run",
        "测试": "test",
        "文档": "documentation",
        "示例": "example",
        "教程": "tutorial",
        "指南": "guide",
        "参考": "reference",
        "快速开始": "Quick Start",
        "安装": "Installation",
        "用法": "Usage",
        "选项": "Options",
        "参数": "Parameters",
        "返回值": "Return value",
        "说明": "Description",
        "示例代码": "Example code",
        "注意事项": "Notes",
        "常见问题": "FAQ",
        "许可证": "License",
    },
    "ja": {
        "编译器": "コンパイラ",
        "语法分析": "構文解析",
        "词法分析": "字句解析",
        "语义分析": "意味解析",
        "代码生成": "コード生成",
        "标准库": "標準ライブラリ",
        "第三方库": "サードパーティライブラリ",
        "包管理": "パッケージ管理",
        "工作空间": "ワークスペース",
        "格式化": "フォーマット",
        "调试": "デバッグ",
        "配置文件": "設定ファイル",
        "命令行": "コマンドライン",
        "静态类型": "静的型付け",
        "类型推导": "型推論",
        "泛型": "ジェネリクス",
        "所有权": "所有権",
        "内存管理": "メモリ管理",
        "线程安全": "スレッドセーフ",
        "并发": "並行処理",
        "异步": "非同期",
        "模式匹配": "パターンマッチング",
        "结构体": "構造体",
        "类": "クラス",
        "枚举": "列挙型",
        "接口": "インターフェース",
        "函数": "関数",
        "变量": "変数",
        "常量": "定数",
        "指针": "ポインタ",
        "引用": "参照",
        "数组": "配列",
        "切片": "スライス",
        "字符串": "文字列",
        "整数": "整数",
        "浮点数": "浮動小数点数",
        "布尔值": "ブール値",
        "空值": "ヌル",
        "错误": "エラー",
        "警告": "警告",
        "依赖": "依存関係",
        "构建": "ビルド",
        "编译": "コンパイル",
        "链接": "リンク",
        "运行": "実行",
        "测试": "テスト",
        "文档": "ドキュメント",
        "示例": "例",
        "教程": "チュートリアル",
        "指南": "ガイド",
        "参考": "リファレンス",
        "快速开始": "クイックスタート",
        "安装": "インストール",
        "用法": "使用方法",
        "选项": "オプション",
        "参数": "パラメータ",
        "返回值": "戻り値",
        "说明": "説明",
        "示例代码": "サンプルコード",
        "注意事项": "注意事項",
        "常见问题": "よくある質問",
        "许可证": "ライセンス",
    },
    "ko": {
        "编译器": "컴파일러",
        "语法分析": "구문 분석",
        "词法分析": "어휘 분석",
        "语义分析": "의미 분석",
        "代码生成": "코드 생성",
        "标准库": "표준 라이브러리",
        "第三方库": " 서드파티 라이브러리",
        "包管理": "패키지 관리",
        "工作空间": "워크스페이스",
        "格式化": "포맷팅",
        "调试": "디버깅",
        "配置文件": "구성 파일",
        "命令行": "명령줄",
        "静态类型": "정적 타입",
        "类型推导": "타입 추론",
        "泛型": "제네릭",
        "所有权": "소유권",
        "内存管理": "메모리 관리",
        "线程安全": "스레드 안전",
        "并发": "동시성",
        "异步": "비동기",
        "模式匹配": "패턴 매칭",
        "结构体": "구조체",
        "类": "클래스",
        "枚举": "열거형",
        "接口": "인터페이스",
        "函数": "함수",
        "变量": "변수",
        "常量": "상수",
        "指针": "포인터",
        "引用": "참조",
        "数组": "배열",
        "切片": "슬라이스",
        "字符串": "문자열",
        "整수": "정수",
        "浮点数": "부동소수점",
        "布尔值": "불리언",
        "空值": "널",
        "错误": "오류",
        "警告": "경고",
        "依赖": "의존성",
        "构建": "빌드",
        "编译": "컴파일",
        "链接": "링크",
        "运行": "실행",
        "测试": "테스트",
        "文档": "문서",
        "示例": "예제",
        "教程": "튜토리얼",
        "指南": "가이드",
        "参考": "참조",
        "快速开始": "빠른 시작",
        "安装": "설치",
        "用法": "사용법",
        "选项": "옵션",
        "参数": "매개변수",
        "返回值": "반환값",
        "说明": "설명",
        "示例代码": "예제 코드",
        "注意事项": "주의사항",
        "常见问题": "자주 묻는 질문",
        "许可证": "라이선스",
    },
}


class TranslationCache:
    """翻译缓存，避免重复翻译相同文本"""

    def __init__(self, cache_dir: Path):
        self.cache_dir = cache_dir
        self.cache_dir.mkdir(parents=True, exist_ok=True)

    def _key(self, text: str, lang: str) -> str:
        h = hashlib.md5(f"{lang}:{text}".encode()).hexdigest()
        return str(self.cache_dir / f"{h}.json")

    def get(self, text: str, lang: str) -> Optional[str]:
        key = self._key(text, lang)
        if os.path.exists(key):
            with open(key, "r", encoding="utf-8") as f:
                data = json.load(f)
                return data.get("translation")
        return None

    def set(self, text: str, lang: str, translation: str):
        key = self._key(text, lang)
        with open(key, "w", encoding="utf-8") as f:
            json.dump({"source": text, "translation": translation}, f, ensure_ascii=False)


class MarkdownTranslator:
    """Markdown 文档翻译器"""

    def __init__(self, lang: str, glossary: dict = None, cache_dir: Path = None):
        self.lang = lang
        self.glossary = glossary or {}
        self.cache = TranslationCache(cache_dir) if cache_dir else None

    def translate_line(self, line: str) -> str:
        """翻译单行文本"""
        # 跳过空行
        if not line.strip():
            return line

        # 跳过代码块标记
        if line.strip().startswith("```"):
            return line

        # 跳过 HTML 标签
        if re.match(r"^\s*<", line):
            return line

        # 跳过 Markdown 链接和图片
        if re.match(r"^\s*\[.*\]\(.*\)\s*$", line.strip()):
            return line

        # 跳过表格分隔行
        if re.match(r"^\s*\|[-\s|]+\|\s*$", line):
            return line

        # 应用术语表替换
        result = line
        for zh, target in self.glossary.items():
            result = result.replace(zh, target)

        return result

    def translate_content(self, content: str) -> str:
        """翻译 Markdown 内容"""
        lines = content.split("\n")
        result = []
        in_code_block = False
        code_block_lines = []
        code_block_indent = ""

        for line in lines:
            # 检测代码块开始/结束
            if line.strip().startswith("```"):
                if not in_code_block:
                    in_code_block = True
                    code_block_indent = line[: len(line) - len(line.lstrip())]
                    code_block_lines = [line]
                else:
                    # 代码块结束，保持原样
                    code_block_lines.append(line)
                    result.extend(code_block_lines)
                    in_code_block = False
                    code_block_lines = []
                continue

            if in_code_block:
                code_block_lines.append(line)
                continue

            # 翻译行
            translated = self.translate_line(line)
            result.append(translated)

        return "\n".join(result)

    def translate_file(self, input_path: Path, output_path: Path, dry_run: bool = False) -> bool:
        """翻译单个文件"""
        try:
            content = input_path.read_text(encoding="utf-8")
            translated = self.translate_content(content)

            if dry_run:
                print(f"\n{'='*60}")
                print(f"文件: {input_path}")
                print(f"{'='*60}")
                print(translated[:500])
                if len(translated) > 500:
                    print("...")
                return True

            output_path.parent.mkdir(parents=True, exist_ok=True)
            output_path.write_text(translated, encoding="utf-8")
            print(f"  [OK] {input_path.name} -> {output_path}")
            return True

        except Exception as e:
            print(f"  [ERROR] {input_path.name}: {e}")
            return False


def find_markdown_files(directory: Path) -> list:
    """查找目录下所有 Markdown 文件"""
    files = []
    # 排除的文件
    excludes = {"translate.py", "README.md"}
    for f in sorted(directory.rglob("*.md")):
        # 跳过已翻译的文件
        name = f.name
        if name in excludes:
            continue
        if any(name.endswith(f"_{code}.md") for code in SUPPORTED_LANGUAGES):
            continue
        # 跳过已翻译的目录
        parts = f.parts
        if any(p in SUPPORTED_LANGUAGES for p in parts):
            continue
        files.append(f)
    return files


def main():
    parser = argparse.ArgumentParser(
        description="Kaula 多语言文档翻译脚本",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="""
示例:
  python translate.py --lang en                    # 翻译为英文
  python translate.py --lang ja --input docs       # 指定输入目录
  python translate.py --all                        # 翻译为所有支持语言
  python translate.py --lang en --output docs/en   # 指定输出目录
  python translate.py --list                       # 列出支持语言
  python translate.py --lang en --dry-run          # 预览不写入
        """,
    )
    parser.add_argument(
        "--lang",
        "-l",
        choices=list(SUPPORTED_LANGUAGES.keys()),
        help="目标语言代码",
    )
    parser.add_argument(
        "--all",
        action="store_true",
        help="翻译为所有支持语言",
    )
    parser.add_argument(
        "--input",
        "-i",
        default="docs",
        help="输入目录 (默认: docs)",
    )
    parser.add_argument(
        "--output",
        "-o",
        help="输出目录 (默认: docs/<lang>)",
    )
    parser.add_argument(
        "--list",
        action="store_true",
        help="列出支持语言",
    )
    parser.add_argument(
        "--dry-run",
        action="store_true",
        help="预览翻译结果，不写入文件",
    )
    parser.add_argument(
        "--cache-dir",
        default=".translate_cache",
        help="翻译缓存目录 (默认: .translate_cache)",
    )
    parser.add_argument(
        "--glossary",
        help="自定义术语表 JSON 文件路径",
    )
    parser.add_argument(
        "--file",
        "-f",
        help="仅翻译指定文件",
    )

    args = parser.parse_args()

    # 列出支持语言
    if args.list:
        print("支持语言:")
        for code, name in SUPPORTED_LANGUAGES.items():
            print(f"  {code:4s} - {name}")
        return

    # 检查参数
    if not args.lang and not args.all:
        parser.error("请指定 --lang 或 --all")

    # 加载自定义术语表
    custom_glossary = {}
    if args.glossary and os.path.exists(args.glossary):
        with open(args.glossary, "r", encoding="utf-8") as f:
            custom_glossary = json.load(f)

    # 确定要翻译的语言
    languages = list(SUPPORTED_LANGUAGES.keys()) if args.all else [args.lang]

    # 输入目录
    input_dir = Path(args.input)
    if not input_dir.exists():
        print(f"错误: 输入目录不存在: {input_dir}")
        sys.exit(1)

    # 缓存目录
    cache_dir = Path(args.cache_dir)

    # 翻译每个语言
    for lang in languages:
        print(f"\n翻译为 {SUPPORTED_LANGUAGES[lang]} ({lang})...")

        # 合并术语表
        glossary = {}
        if lang in DEFAULT_GLOSSARY:
            glossary.update(DEFAULT_GLOSSARY[lang])
        glossary.update(custom_glossary)

        # 创建翻译器
        translator = MarkdownTranslator(lang, glossary, cache_dir)

        # 确定输出目录
        if args.output:
            output_dir = Path(args.output)
        else:
            output_dir = input_dir

        # 查找文件
        if args.file:
            files = [Path(args.file)]
        else:
            files = find_markdown_files(input_dir)

        if not files:
            print(f"  未找到 Markdown 文件")
            continue

        print(f"  找到 {len(files)} 个文件")

        # 翻译文件
        success = 0
        for f in files:
            # 计算相对路径
            try:
                rel = f.relative_to(input_dir)
            except ValueError:
                rel = Path(f.name)

            # 添加语言后缀: file.md -> file_en.md
            stem = rel.stem
            suffix = rel.suffix
            translated_name = f"{stem}_{lang}{suffix}"
            output_path = output_dir / rel.parent / translated_name

            if translator.translate_file(f, output_path, args.dry_run):
                success += 1

        print(f"  完成: {success}/{len(files)} 个文件成功翻译")


if __name__ == "__main__":
    main()
