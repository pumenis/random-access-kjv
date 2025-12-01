SELECT
  books.book_number,
  long_name,
  COUNT(*) AS verse_count
FROM
  verses INNER JOIN books ON verses.book_number = books.book_number
-- WHERE  books.book_number >= 470
GROUP BY
  verses.book_number
ORDER BY
  verses.book_number
