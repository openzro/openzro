// ADR-0026 D3 — the Allowed email domains card on Settings →
// Authentication saves settings.allowed_email_domains through
// PUT /api/accounts/{id}. Needs the dev stack (`make dev.dashboard`).
// The spec restores the list it found.

describe("Allowed email domains", () => {
  beforeEach(() => {
    cy.login();
  });

  it("adds a normalized domain and removes it again", () => {
    const domain = `cy-${Date.now()}.example.org`;
    cy.intercept("PUT", "/api/accounts/*").as("save");
    cy.visit("/settings/authentication");

    cy.get("[data-cy=allowed-domain-input]").type(
      `  @${domain.toUpperCase()}{enter}`,
    );
    cy.get("[data-cy=allowed-domain-list]").should("contain", domain);
    cy.get("[data-cy=save-authentication-settings]").click();
    cy.wait("@save").then(({ request, response }) => {
      expect(request.body.settings.allowed_email_domains).to.include(domain);
      expect(response?.statusCode).to.eq(200);
    });

    cy.get(`[aria-label='Remove ${domain}']`).click();
    cy.get("[data-cy=save-authentication-settings]").click();
    cy.wait("@save").then(({ request }) => {
      expect(request.body.settings.allowed_email_domains).not.to.include(
        domain,
      );
    });
  });
});
