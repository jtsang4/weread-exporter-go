# Generates testdata/normalize_golden.json by running the ORIGINAL Python
# pre-processing (the text part of WeReadExporter.pre_process_markdown from
# weread_exporter/export.py, copied verbatim) over a set of inputs.
#
#   python3 testdata/generators/normalize.py > testdata/normalize_golden.json
#
# The Go port (internal/markdown.Normalize) must reproduce these outputs exactly.
import json


def pre_process(text: str) -> str:
    output = ""
    code_mode = False
    blank_line = False
    for line in text.split("\n"):
        if line == "```":
            if not code_mode:
                output += "\n%s\n" % line
            else:
                output += "%s\n" % line
            code_mode = not code_mode
        elif code_mode:
            output += line + "\n"
        elif line == "":
            blank_line = True
        elif blank_line:
            output += "\n\n%s" % line
            blank_line = False
        else:
            output += line
    output += "\n"
    return output


CASES = {
    "empty": "",
    "single newline": "\n",
    "single line": "hello",
    "wrapped paragraph (english rows keep their trailing space)": "The quick brown fox \njumps over the lazy \ndog.\n",
    "wrapped paragraph (cjk rows)": "这是第一行文字，\n这是第二行文字。\n",
    "two paragraphs": "first row\nsecond row\n\nthird row\n",
    "many blank lines collapse": "a\n\n\n\n\nb\n",
    "leading blank lines": "\n\n\na\nb\n",
    "trailing blank lines": "a\nb\n\n\n\n",
    "heading then text": "# Title\n\nText row one\ntext row two\n\n## Sub\n\nMore\n",
    "code block": "before\n```\nprint('hi')\n\n  indented\n```\nafter\n",
    "code block directly after a blank line": "before\n\n```\nx = 1\n```\n\nafter\n",
    "code block at start": "```\nx\n```\n",
    "two code blocks": "a\n```\none\n```\nb\n```\ntwo\n```\nc\n",
    "unclosed code block swallows the rest": "a\n```\ncode\n\nmore code\n",
    "fence with language is ordinary text": "a\n```python\nb\n```\nc\n",
    "fence with trailing space is ordinary text": "a\n``` \nb\n",
    "blank lines inside code are kept": "```\na\n\n\nb\n```\n",
    "image line": "text\n\n![](https://cdn.example.com/a.jpg)\n\nmore\n",
    "inline markup": "a **bold** and `code` and <sup>1</sup>\nnext row\n",
    "windows line endings are not special": "a\r\nb\r\n\r\nc\r\n",
    "only blank lines": "\n\n\n",
    "list-like rows": "- one\n- two\n\n- three\n",
    "unicode": "emoji 😀 row\nnext — row\n",
    "long mixed document": "# Chapter\n\nIntro row one \nintro row two\n\n```\ncode()\n```\n\n- a\n- b\n\n## Section\n\ntext 1\ntext 2\n\n",
}

out = [{"name": name, "in": text, "out": pre_process(text)} for name, text in CASES.items()]
print(json.dumps(out, indent=1, ensure_ascii=False))
