# Loan agreement daily snapshots

`lnm_agreement_daily` stores one tenant-scoped, end-of-day balance row per loan
agreement and business date. The primary key is `(agreement_id, data_date)`;
the EOD retry upserts that date so it cannot create duplicate history rows.

The Platform EOD seed registers `LNM_AGREEMENT_DAILY_SNAPSHOT` after
`LNM_PROVISION_DAILY` and makes `FIN_TRIAL_BALANCE_DAILY` depend on it. The loan
worker receives the EOD `to_date` at
`POST /internal/jobs/agreement-daily-snapshot?to_date=YYYY-MM-DD`, requires the
tenant header, and captures all agreements belonging to that tenant. Snapshot
reads request an exact date; they do not silently return an older row when an
EOD date is missing.

The snapshot includes currency, disbursement and pending-disbursement amounts,
outstanding principal, collected principal and interest, provision, and the
three off-balance amounts. Current off-balance amounts live on
`lnm_agreements` as non-negative minor-unit integers.

`lnm_journal_link` records the posted finance entry for a business transaction,
including the voucher display number and posted timestamp. A business
transaction type/ID pair can be linked once. This task adds the storage
contract; adjustment posting flows will populate it in T8.1.

The snapshot EOD step is local application code seeded through the existing
Platform `SeedJobs` flow. No migration or seed was applied to the shared Arda
database as part of this task.
