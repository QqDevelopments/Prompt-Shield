import { useQuery } from "@tanstack/react-query";
import { LuTerminal } from "react-icons/lu";
import { request, type Overview } from "../../app/api";
import { CopyButton, ErrorNotice, Heading } from "../../components/ui";
import { usePreferences } from "../preferences/Preferences";

export function IntegrationScreen() {
  const { t } = usePreferences();
  const overview = useQuery({
    queryKey: ["overview"],
    queryFn: ({ signal }) => request<Overview>("/overview", { signal }),
  });
  const base = overview.data?.base_url ?? `${location.origin}/v1`;
  const example = `from openai import OpenAI\n\nclient = OpenAI(\n    base_url=${JSON.stringify(base)},\n    api_key="YOUR_PROMPT_SHIELD_KEY",\n)\n\nresponse = client.chat.completions.create(\n    model="YOUR_PROVIDER_MODEL",\n    messages=[{"role": "user", "content": "Hello"}],\n)\nprint(response.choices[0].message.content)`;
  const anthropicExample = `from anthropic import Anthropic\n\nclient = Anthropic(\n    base_url=${JSON.stringify(base.replace(/\/v1$/, ""))},\n    api_key="YOUR_PROMPT_SHIELD_KEY",\n)\n\nresponse = client.messages.create(\n    model="YOUR_PROVIDER_MODEL",\n    max_tokens=128,\n    messages=[{"role": "user", "content": "Hello"}],\n)\nprint(response.content[0].text)`;
  const geminiExample = `from openai import OpenAI\n\nclient = OpenAI(\n    base_url=${JSON.stringify(`${base}/gemini`)},\n    api_key="YOUR_PROMPT_SHIELD_KEY",\n)\n\nresponse = client.chat.completions.create(\n    model="YOUR_GEMINI_MODEL",\n    max_tokens=128,\n    messages=[{"role": "user", "content": "Hello"}],\n    extra_headers={"X-Provider-API-Key": "YOUR_GEMINI_API_KEY"},\n)\nprint(response.choices[0].message.content)`;
  return (
    <>
      <Heading
        title={t("integration.title")}
        description={t("integration.description")}
      />
      <ErrorNotice
        error={overview.error}
        onRetry={() => void overview.refetch()}
      />
      <section className="panel integration-base">
        <label>{t("integration.base")}</label>
        <div className="code-row">
          <code>{base}</code>
          <CopyButton text={base} />
        </div>
      </section>
      <section className="panel">
        <div className="panel-heading">
          <h2>{t("integration.providers")}</h2>
        </div>
        {[
          { name: "OpenAI", configured: overview.data?.openai_configured },
          {
            name: "Anthropic",
            configured: overview.data?.anthropic_configured,
          },
          {
            name: "Google Gemini",
            configured: overview.data?.gemini_configured,
          },
        ].map(({ name, configured }) => (
          <div className="provider-row" key={name}>
            <strong>{name}</strong>
            <span>
              {t(
                configured
                  ? "integration.configured"
                  : "integration.unconfigured",
              )}
            </span>
          </div>
        ))}
      </section>
      <p className="notice">{t("integration.streaming")}</p>
      <section className="panel">
        <div className="panel-heading">
          <h2>
            <LuTerminal size={17} />
            {t("integration.example")}
          </h2>
          <CopyButton text={example} />
        </div>
        <pre className="code-example">{example}</pre>
      </section>
      <p className="caption">{t("integration.secret")}</p>
      <section className="panel">
        <div className="panel-heading">
          <h2>
            <LuTerminal size={17} />
            Anthropic
          </h2>
          <CopyButton text={anthropicExample} />
        </div>
        <pre className="code-example">{anthropicExample}</pre>
      </section>
      <section className="panel">
        <div className="panel-heading">
          <h2>
            <LuTerminal size={17} />
            Google Gemini
          </h2>
          <CopyButton text={geminiExample} />
        </div>
        <pre className="code-example">{geminiExample}</pre>
      </section>
    </>
  );
}
