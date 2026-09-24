import { access, readFile } from "node:fs/promises";
import path from "node:path";
import process from "node:process";
import remarkParse from "remark-parse";
import { unified } from "unified";

const root = process.cwd();
const readmePath = path.join(root, "README.md");
const expectedHeadings = [
  "Install",
  "Quick start",
  "Runtime model",
  "Protocols",
  "Security boundary",
  "Configuration",
  "Operations",
  "Observability",
  "Documentation",
  "Development",
  "Project boundary",
];

function walk(node, visit) {
  visit(node);
  for (const child of node.children ?? []) walk(child, visit);
}

async function exists(relativePath) {
  try {
    await access(relativePath);
    return true;
  } catch {
    return false;
  }
}

function fail(message) {
  console.error(message);
  process.exitCode = 1;
}

const source = await readFile(readmePath, "utf8");
const tree = unified().use(remarkParse).parse(source);
const headings = [];
const links = [];
const codeBlocks = [];

walk(tree, (node) => {
  if (node.type === "heading") {
    headings.push({
      depth: node.depth,
      text: node.children.map((child) => child.value).join(""),
    });
  }
  if (node.type === "link") links.push(node.url);
  if (node.type === "code") codeBlocks.push(node);
});

const h1 = headings.filter(({ depth }) => depth === 1);
const h2 = headings.filter(({ depth }) => depth === 2);

if (h1.length !== 1 || h1[0].text !== "Liki Agents") {
  fail("README must contain exactly one H1: Liki Agents");
}

if (JSON.stringify(h2.map(({ text }) => text)) !== JSON.stringify(expectedHeadings)) {
  fail(
    `README H2 sequence mismatch:\n actual:   ${h2.map(({ text }) => text).join(" | ")}\n expected: ${expectedHeadings.join(" | ")}`,
  );
}

if (headings.some(({ depth }) => depth > 3)) fail("README headings must not be deeper than H3");
const lineCount = source.split("\n").filter((line, index, lines) => index < lines.length - 1 || line !== "").length;
if (lineCount >= 180) fail("README must stay below 180 lines");
if (!source.includes("generic multi-agent runtime")) {
  fail("README must state the generic multi-agent runtime positioning");
}

for (const url of links) {
  if (URL.canParse(url) && new URL(url).protocol.startsWith("http")) continue;
  if (!(await exists(path.resolve(root, decodeURI(url))))) fail(`missing local link target: ${url}`);
}

for (const block of codeBlocks) {
  if (!block.lang) fail(`fenced code block near line ${block.position.start.line} must declare a language`);
}
