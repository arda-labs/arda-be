-- +goose Up

-- The standalone /ai page is retired: the assistant is reached through the
-- shell's "Hỏi AI" dock panel (Ctrl+J) instead. Hide the global default menu
-- entry so the sidebar no longer links to a route that no longer exists.

UPDATE plt_menus
SET is_active = false,
    updated_at = now()
WHERE code = 'ai'
  AND tenant_id IS NULL;

-- +goose Down

UPDATE plt_menus
SET is_active = true,
    updated_at = now()
WHERE code = 'ai'
  AND tenant_id IS NULL;
