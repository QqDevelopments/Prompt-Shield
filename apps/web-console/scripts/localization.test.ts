import assert from "node:assert/strict";
import { readdirSync, readFileSync } from "node:fs";
import { dirname, join, relative } from "node:path";
import { fileURLToPath } from "node:url";

const rootDir = dirname(dirname(fileURLToPath(import.meta.url)));
const localeDir = join(rootDir, "src", "localization");
const pendingKeysPath = join(
  rootDir,
  "scripts",
  "pending-localization-keys.json",
);
const englishFileName = "en.json";
const localeFiles = readdirSync(localeDir)
  .filter((fileName) => fileName.endsWith(".json"))
  .sort();

assert.ok(
  localeFiles.includes(englishFileName),
  "en.json must be the localization root",
);

const english = JSON.parse(
  readFileSync(join(localeDir, englishFileName), "utf8"),
) as Record<string, string>;
const englishKeysArray = Object.keys(english);
const sortedEnglishKeys = [...englishKeysArray].sort();
assert.deepEqual(
  englishKeysArray,
  sortedEnglishKeys,
  "en.json keys must be sorted alphabetically",
);
const englishKeys = new Set(englishKeysArray);
const pendingKeys = JSON.parse(
  readFileSync(pendingKeysPath, "utf8"),
) as string[];
assert.ok(
  Array.isArray(pendingKeys),
  `${relative(rootDir, pendingKeysPath)} must be an array`,
);
for (const key of pendingKeys) {
  assert.ok(
    englishKeys.has(key),
    `Pending localization key ${key} must exist in en.json`,
  );
}
const pendingKeySet = new Set(pendingKeys);
const mojibakePattern = /[A-Za-zÀ-ÿ]\?[A-Za-zÀ-ÿ]|\uFFFD/;

let pendingTranslationCount = 0;
for (const fileName of localeFiles) {
  const localePath = join(localeDir, fileName);
  const locale = JSON.parse(readFileSync(localePath, "utf8")) as Record<
    string,
    string
  >;
  for (const [key, value] of Object.entries(locale)) {
    assert.equal(
      typeof value,
      "string",
      `${relative(rootDir, localePath)} key ${key} must have a string value`,
    );
    if (typeof value === "string") {
      assert.equal(
        mojibakePattern.test(value),
        false,
        `${relative(rootDir, localePath)} key ${key} contains suspicious mojibake`,
      );
    }
  }
  if (fileName !== englishFileName) {
    const localeKeys = Object.keys(locale);
    const expectedKeys = englishKeysArray.filter(
      (key) => !pendingKeySet.has(key) || key in locale,
    );
    assert.deepEqual(
      localeKeys,
      expectedKeys,
      `${relative(
        rootDir,
        localePath,
      )} must have exactly the same keys in the exact same alphabetical order as en.json (except pending translations)`,
    );
    const missingRequiredKeys = [...englishKeys].filter(
      (key) => !pendingKeySet.has(key) && !(key in locale),
    );
    assert.deepEqual(
      missingRequiredKeys,
      [],
      `${relative(
        rootDir,
        localePath,
      )} is missing required localization keys: ${missingRequiredKeys.join(", ")}`,
    );
    pendingTranslationCount += pendingKeys.filter(
      (key) => !(key in locale),
    ).length;
  }
}

console.log(
  `localization ok: root=en.json locales=${localeFiles.length} keys=${englishKeys.size}`,
);
if (pendingKeys.length > 0) {
  console.log(
    `localization pending translations: keys=${pendingKeys.length} locales=${
      localeFiles.length - 1
    } missing=${pendingTranslationCount}; listed in scripts/pending-localization-keys.json`,
  );
}
