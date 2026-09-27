// Model output as Markdown. It becomes React elements (react-markdown),
// never raw HTML: HTML in the text shows as text. Links open in the system
// browser after a click, never in the window, and images aren't loaded
// (output can't make the app fetch anything); they show as links.
import { memo, useState, type ReactNode } from "react";
import ReactMarkdown, { type Components } from "react-markdown";
import remarkGfm from "remark-gfm";
import { mdiCheck, mdiContentCopy, mdiImageOutline, mdiOpenInNew } from "@mdi/js";
import { openURL } from "./desktop";
import { highlight, languageFor } from "./highlight";
import { t, useLanguage } from "./i18n";
import { Icon, IconButton, useSnackbar } from "./ui/controls";

function Link({ href, children }: { href?: string; children?: ReactNode }) {
  const snack = useSnackbar();
  if (!href) return <>{children}</>;
  return (
    <a
      href={href}
      title={href}
      onClick={(e) => {
        e.preventDefault();
        openURL(href).catch((err) => snack(String(err), { error: true }));
      }}
    >
      {children}
      <Icon path={mdiOpenInNew} size="sm" className="link-icon" />
    </a>
  );
}

function CodeBlock({ children, lang }: { children: string; lang: string }) {
  const [copied, setCopied] = useState(false);
  return (
    <div className="code-block">
      <div className="code-head">
        <span className="t-label muted">{lang || "text"}</span>
        <IconButton
          icon={copied ? mdiCheck : mdiContentCopy}
          label={copied ? t("desktop.code.copied") : t("desktop.code.copy")}
          small
          onClick={() =>
            navigator.clipboard?.writeText(children).then(() => {
              setCopied(true);
              setTimeout(() => setCopied(false), 1500);
            })
          }
        />
      </div>
      <pre>
        <code className="hljs">{highlight(children, languageFor(lang))}</code>
      </pre>
    </div>
  );
}

const components: Components = {
  a: ({ href, children }) => <Link href={href}>{children}</Link>,
  img: ({ src, alt }) =>
    typeof src === "string" && src ? (
      <Link href={src}>
        <Icon path={mdiImageOutline} size="sm" /> {alt || "image"}
      </Link>
    ) : (
      <span>{alt}</span>
    ),
  pre: ({ children }) => <>{children}</>,
  code: ({ className, children }) => {
    const text = String(children ?? "");
    const lang = /language-([\w+-]+)/.exec(className ?? "")?.[1];
    // Fenced blocks have a language or span lines; the rest is inline code.
    if (lang || text.includes("\n")) return <CodeBlock lang={lang ?? ""}>{text.replace(/\n$/, "")}</CodeBlock>;
    return <code className="inline-code">{children}</code>;
  },
  table: ({ children }) => (
    <div className="table-wrap">
      <table>{children}</table>
    </div>
  ),
};

export const Markdown = memo(function Markdown({ text }: { text: string }) {
  useLanguage();
  return (
    <div className="markdown">
      <ReactMarkdown remarkPlugins={[remarkGfm]} components={components} skipHtml>
        {text}
      </ReactMarkdown>
    </div>
  );
});
