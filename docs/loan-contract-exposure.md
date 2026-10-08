# Loan contract disbursement exposure

REGISTER headroom has one definition in `internal/domain.ContractExposure`:

```text
headroom = contract loan amount - outstanding - pending - held reservations
```

- **Outstanding** is principal already transferred to the borrower and still owed.
- **Pending** is REGISTER principal posted to the loan ledger but waiting for the COMPLETE transfer.
- **Held reservations** are REGISTER requests in DRAFT, SUBMITTED, or APPROVED processing; rejected and cancelled requests release their reservation.

The three amounts are disjoint. REGISTER settlement increases `pending` only. COMPLETE settlement transfers its amount from `pending` to `outstanding`. A REGISTER creation inserts its disbursement row and HELD reservation in one transaction. The transaction locks the contract row before reading exposure, so concurrent single and batch requests serialize on the same limit. Settlement changes HELD to CONSUMED in the same transaction as posting; rejection/cancellation changes HELD to RELEASED with the decision.

Migration `20261008120000_lnm_contract_reservations.sql` converts legacy balances created by the previous REGISTER logic. That logic added the same drawdown to both `outstanding` and `pending`; the migration subtracts pending from outstanding, preserving their sum. If any agreement has negative pending or pending greater than outstanding, or a contract is already over its limit after active reservations are included, migration aborts instead of guessing. Existing REGISTER rows are backfilled as HELD, CONSUMED, or RELEASED according to their current status.

Run the DB-backed service tests with PostgreSQL 18 and `ARDA_TEST_DSN` set. The migration regression test seeds the legacy 400/400 balance, runs the migration, verifies it becomes 0/400, then confirms a 500 REGISTER fits a 1,000 contract.
