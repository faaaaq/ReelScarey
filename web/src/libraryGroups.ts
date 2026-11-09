/** Matches the server's title sort: ignore one leading "The ". */
export function firstLetter(title: string) {
  const trimmed = title
    .trim()
    .replace(/^the\s+/i, "")
    .trimStart();
  const letter = trimmed.charAt(0).toUpperCase();
  return /^[A-Z]$/.test(letter) ? letter : "#";
}

export function groupedTitles<T extends { title: string }>(items: T[]) {
  return groupedByLetter(items, (item) => item.title);
}

export function groupedByLetter<T>(
  items: T[],
  title: (item: T) => string,
): [string, T[]][] {
  const groups = new Map<string, T[]>();
  for (const item of items) {
    const letter = firstLetter(title(item));
    const rows = groups.get(letter);
    if (rows) rows.push(item);
    else groups.set(letter, [item]);
  }
  return [...groups.entries()];
}

export function groupID(letter: string) {
  return `lib-letter-${letter === "#" ? "other" : letter.toLowerCase()}`;
}

export function sortLetters(letters: string[]) {
  return [...letters].sort((a, b) =>
    a === "#" ? 1 : b === "#" ? -1 : a.localeCompare(b),
  );
}
