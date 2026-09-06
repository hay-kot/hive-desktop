// GitHub-flavored alert blockquotes as callouts:
//
//   > [!TIP] Optional title
//   > Body text.
//
// The marker line becomes the callout's title (the kind's name when no title
// is given) and the blockquote is emitted as <aside class="callout callout--tip">.
// Any other blockquote is left alone. The syntax was chosen because it also
// renders acceptably on GitHub, where the same Markdown is read in review.

const KINDS = new Map([
  ["NOTE", "Note"],
  ["TIP", "Tip"],
  ["IMPORTANT", "Important"],
  ["WARNING", "Warning"],
  ["CAUTION", "Caution"],
]);

const MARKER = /^\[!([A-Z]+)\][ \t]*([^\n]*)\n?/;

export default function remarkCallouts() {
  return (tree) => visit(tree);
}

function visit(node) {
  if (!node.children) return;
  for (const child of node.children) {
    if (child.type === "blockquote") transform(child);
    visit(child);
  }
}

function transform(quote) {
  const first = quote.children[0];
  if (!first || first.type !== "paragraph") return;
  const text = first.children[0];
  if (!text || text.type !== "text") return;

  const match = MARKER.exec(text.value);
  if (!match) return;
  const kind = match[1];
  const label = KINDS.get(kind);
  if (!label) return;

  text.value = text.value.slice(match[0].length);
  if (text.value === "") first.children.shift();
  if (first.children.length === 0) quote.children.shift();

  const title = match[2].trim() || label;
  quote.children.unshift({
    type: "paragraph",
    data: { hName: "p", hProperties: { className: ["callout__title"] } },
    children: [{ type: "text", value: title }],
  });
  quote.data = {
    hName: "aside",
    hProperties: { className: ["callout", `callout--${kind.toLowerCase()}`] },
  };
}
