UPDATE listings
SET country_code = 'CO'
WHERE country_code IS NULL
  AND lower(country) = 'colombia';
