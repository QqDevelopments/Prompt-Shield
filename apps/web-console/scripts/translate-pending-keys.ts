import { existsSync, readdirSync, readFileSync, writeFileSync } from "node:fs";
import { join, resolve } from "node:path";

type Locale = Record<string, string>;

const pendingKeysPath = resolve("scripts/pending-localization-keys.json");
const localesDir = resolve("src/localization");

const languageAliases: Record<string, string> = {
  zh: "zh-CN",
};

const USER_AGENT =
  "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/122.0.0.0 Safari/537.36";

const sleep = (milliseconds: number): Promise<void> =>
  new Promise((resolveSleep) => setTimeout(resolveSleep, milliseconds));

const fetchGoogleTranslate = async (
  text: string,
  targetLang: string,
  client: string,
): Promise<string> => {
  const url =
    `https://translate.googleapis.com/translate_a/single?client=${encodeURIComponent(
      client,
    )}&sl=en` +
    `&tl=${encodeURIComponent(targetLang)}&dt=t&q=${encodeURIComponent(text)}`;
  const response = await fetch(url, {
    headers: {
      "User-Agent": USER_AGENT,
      Accept: "application/json, text/plain, */*",
    },
  });
  if (!response.ok) {
    throw new Error(`Google Translate (${client}) HTTP ${response.status}`);
  }
  const json = (await response.json()) as unknown;
  if (!Array.isArray(json) || !Array.isArray(json[0])) {
    throw new Error("Unexpected Google Translate response format");
  }
  return json[0]
    .map((item: unknown) =>
      Array.isArray(item) && typeof item[0] === "string" ? item[0] : "",
    )
    .join("");
};

const fetchMyMemory = async (
  text: string,
  targetLang: string,
): Promise<string> => {
  const url = `https://api.mymemory.translated.net/get?q=${encodeURIComponent(
    text,
  )}&langpair=en|${encodeURIComponent(targetLang)}`;
  const response = await fetch(url, {
    headers: {
      "User-Agent": USER_AGENT,
    },
  });
  if (!response.ok) {
    throw new Error(`MyMemory HTTP ${response.status}`);
  }
  const json = (await response.json()) as {
    responseData?: { translatedText?: string };
  };
  if (
    json &&
    json.responseData &&
    typeof json.responseData.translatedText === "string"
  ) {
    return json.responseData.translatedText;
  }
  throw new Error("MyMemory invalid response");
};

const translateSingle = async (
  text: string,
  targetLang: string,
): Promise<string> => {
  const language = languageAliases[targetLang] ?? targetLang;
  if (language === "en") return text;

  const clients = ["dict-chrome-ex", "gtx"];
  for (const client of clients) {
    try {
      const result = await fetchGoogleTranslate(text, language, client);
      if (result && result.trim()) return result.trim();
    } catch {
      // Fall back to next client
    }
  }

  try {
    const result = await fetchMyMemory(text, language);
    if (result && result.trim()) return result.trim();
  } catch {
    // Fall back
  }

  throw new Error(`Failed to translate "${text}" to ${targetLang}`);
};

const translateBatch = async (
  texts: string[],
  targetLang: string,
): Promise<string[]> => {
  const language = languageAliases[targetLang] ?? targetLang;
  if (language === "en") return texts;

  const joinedText = texts.join("\n");
  const clients = ["dict-chrome-ex", "gtx"];

  for (const client of clients) {
    for (let attempt = 1; attempt <= 3; attempt += 1) {
      try {
        const fullTranslated = await fetchGoogleTranslate(
          joinedText,
          language,
          client,
        );
        const splitTranslated = fullTranslated.split("\n").map((s) => s.trim());
        if (splitTranslated.length === texts.length) {
          return splitTranslated;
        }
        console.warn(
          `Warning: Length mismatch for ${targetLang} with ${client}. Expected ${texts.length}, got ${splitTranslated.length}.`,
        );
        break;
      } catch {
        if (attempt < 3) {
          await sleep(attempt * 1000);
        }
      }
    }
  }

  console.log(`Falling back to individual translations for ${targetLang}...`);
  const results: string[] = [];
  for (const t of texts) {
    results.push(await translateSingle(t, targetLang));
    await sleep(200);
  }
  return results;
};

const restorePlaceholders = (translated: string, original: string): string => {
  const placeholders = original.match(/\{\{[a-zA-Z0-9_]+\}\}/g);
  if (!placeholders) return translated;

  let placeholderIndex = 0;
  return translated.replace(
    /\{\s*\{\s*[^{}]+?\s*\}\s*\}/g,
    () => placeholders[placeholderIndex++] ?? placeholders[0],
  );
};

const readJson = <T>(filePath: string): T =>
  JSON.parse(readFileSync(filePath, "utf8")) as T;

const run = async (): Promise<void> => {
  let pendingKeys: string[];
  try {
    pendingKeys = readJson<string[]>(pendingKeysPath);
  } catch {
    console.error("Could not read pending-localization-keys.json");
    return;
  }

  if (
    !Array.isArray(pendingKeys) ||
    !pendingKeys.every((key) => typeof key === "string")
  ) {
    throw new Error(
      "scripts/pending-localization-keys.json must be a string array",
    );
  }

  if (pendingKeys.length === 0) {
    console.log("No pending localization keys found.");
    return;
  }

  if (!existsSync(localesDir)) {
    console.log("No localization directory found.");
    return;
  }

  const enPath = join(localesDir, "en.json");
  if (!existsSync(enPath)) {
    console.warn(`Warning: ${enPath} not found.`);
    return;
  }

  const english = readJson<Locale>(enPath);
  const englishValues = pendingKeys.map((key) => {
    const value = english[key];
    if (!value) {
      console.warn(`Warning: Key "${key}" not found in en.json`);
    }
    return value ?? "";
  });

  const localeFiles = readdirSync(localesDir)
    .filter((fileName) => fileName.endsWith(".json") && fileName !== "en.json")
    .sort();

  let allSucceeded = true;

  for (const fileName of localeFiles) {
    const lang = fileName.slice(0, -5);
    console.log(`Processing ${fileName}...`);

    const filePath = join(localesDir, fileName);
    const locale = readJson<Locale>(filePath);

    try {
      const translatedValues = await translateBatch(englishValues, lang);
      let changed = false;

      pendingKeys.forEach((key, index) => {
        const englishValue = englishValues[index];
        if (!englishValue) return;

        const translatedValue = restorePlaceholders(
          translatedValues[index],
          englishValue,
        );
        if (locale[key] !== translatedValue) {
          locale[key] = translatedValue;
          changed = true;
        }
      });

      if (changed) {
        const sortedLocale = Object.fromEntries(
          Object.entries(locale).sort(([leftKey], [rightKey]) =>
            leftKey < rightKey ? -1 : leftKey > rightKey ? 1 : 0,
          ),
        );
        writeFileSync(
          filePath,
          `${JSON.stringify(sortedLocale, null, 2)}\n`,
          "utf8",
        );
        console.log(`Updated ${fileName}`);
      }
    } catch (error) {
      const message = error instanceof Error ? error.message : String(error);
      console.error(`Error translating for ${fileName}:`, message);
      allSucceeded = false;
    }

    await sleep(500);
  }

  if (allSucceeded) {
    writeFileSync(pendingKeysPath, "[]\n", "utf8");
    console.log("Pending localization keys translated and cleared.");
  } else {
    console.warn(
      "Some translations failed. Pending keys file was NOT cleared.",
    );
  }
};

run().catch((error: unknown) => {
  console.error(error);
  process.exitCode = 1;
});
