#!/usr/bin/env bash
set -euo pipefail

DB="$HOME/.local/share/mybible/KJV.SQLite3"                   # path to your SQLite file
OUT_DIR="./bible-txt"             # where to write the .txt files
mkdir -p "$OUT_DIR"

# 1. Fetch every distinct book name, ordered alphabetically.
mapfile -t BOOKS < <(
  sqlite3 "$DB" \
    "SELECT book_number FROM books -- WHERE book_number >= 470"
)

# 2. For each book, export its verses as "chapter:verse text"
for book in "${BOOKS[@]}"; do
  # Sanitize the filename: replace spaces and slashes with underscores
  safeName=$(echo "$book" | sed -e 's#[ /]#_#g' -e 's/[^[:alnum:]_.-]//g')
  outfile="$OUT_DIR/${safeName}.txt"

  echo "Exporting \"$book\" → $outfile"
  sqlite3 "$DB" <<SQL > "$outfile"
SELECT
  chapter || ':' || verse || ' ' || text
FROM
  verses
WHERE
  book_number = '$book'
ORDER BY
  chapter, verse;
SQL
done

echo "All books exported to $OUT_DIR/*.txt"
echo "Exporting index.txt"
cat new.sql |  sqlite3 "$DB" > "index.txt"

