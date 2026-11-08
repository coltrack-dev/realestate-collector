# Source research (2026-11-08)

## Ciencuadras

- Public search page observed: `https://www.ciencuadras.com/venta/bogota/apartamento`; search results expose listing links and external-looking IDs in rendered HTML.
- Anonymous access is sufficient to view the search page and cards; registration is not required for the observed public results. No account, browser profile or cookie was used.
- `robots.txt` allows some public paths but disallows several paths/query patterns. This is not permission to scrape.
- Terms and Conditions, section 7, expressly prohibit robots/spiders/scripts and extracting, compiling or analysing platform content, and prohibit collecting platform information without prior express authorisation.
- Therefore the live adapter is deliberately disabled. No CAPTCHA, authentication, anti-bot or access restriction is bypassed. MVP uses reproducible local JSON fixtures.

## FincaRaíz (reserve)

- Public search pages were discoverable and rendered listing fields (price, area, rooms, bathrooms, description and links).
- Anonymous access was sufficient for the observed results; registration was not required and no account was created.
- A reliable official robots/terms permission for the proposed automated mode was not confirmed during this run; third-party terms copies are not sufficient evidence.
- No adapter is enabled until the owner grants permission or an explicitly allowed API/feed is identified.

## Authentication policy

`AUTH_MODE=none` is the default and the only active MVP mode. Since listings are publicly viewable, login is intentionally excluded. `AUTH_MODE=session` is only a future contract for an explicitly authorized source; it does not load cookies, browser profiles, Google OAuth, CAPTCHA flows or personal sessions. Secrets belong in environment variables or a protected secret store, never Git.

## Data and operational notes

The observed pages contain enough public listing data for a future permitted adapter, but detail-page JSON-LD and pagination must be revalidated under an authorised access method. Likely IDs are embedded in listing URLs/cards; the fixture parser treats the source-provided ID as the stable key. Blocking risk is high if automated access is attempted without permission. No live listings were collected by this MVP.
