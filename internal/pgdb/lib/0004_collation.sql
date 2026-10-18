-- <prefix>_lib, part 4: abi_ci, the case-insensitive collation. Text columns
-- are binary ("C"), as SQLite's are, so = and < compare exactly; abi_ci is used
-- only where SQLite says COLLATE NOCASE: sorting, the indexes that serve it
-- (unique text indexes are therefore case-insensitive) and upsert targets.
-- ICU level 2 also ignores case outside ASCII, which NOCASE does not.
CREATE COLLATION IF NOT EXISTS @lib.abi_ci (provider = icu, locale = 'und-u-ks-level2', deterministic = false);
