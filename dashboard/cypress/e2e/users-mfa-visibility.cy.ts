// Admin 2FA visibility: the Users list shows each user's 2FA state and
// Settings → Authentication shows how many active users have no 2FA under
// each toggle. Needs the dev stack (`make dev.dashboard`). Read-only.

describe("2FA visibility for admins", () => {
  beforeEach(() => {
    cy.login();
  });

  it("shows each user's 2FA state in the Users list", () => {
    cy.intercept("GET", "/api/users*").as("users");
    cy.visit("/team/users");
    cy.wait("@users").then(({ response }) => {
      const users: { mfa_enrolled?: boolean; is_service_user?: boolean }[] =
        response?.body ?? [];
      expect(users.length).to.be.greaterThan(0);
      users
        .filter((u) => !u.is_service_user)
        .forEach((u) => expect(u.mfa_enrolled).to.be.a("boolean"));
    });
    cy.contains("th", "2FA", { timeout: 20000 }).should("be.visible");
    cy.get("tbody tr")
      .first()
      .within(() => {
        cy.contains(/^(On|Off|Locked until \d\d:\d\d)$/).should("exist");
      });
  });

  it("shows the unenrolled count under each 2FA toggle", () => {
    cy.intercept("GET", "/api/accounts").as("accounts");
    cy.visit("/settings/authentication");
    cy.wait("@accounts").then(({ response }) => {
      const gaps = response?.body?.[0]?.mfa_unenrolled_users;
      expect(gaps).to.have.keys("local", "federated");
      const expected = (count: number, who: string) =>
        count === 0
          ? `No active ${who} user is missing 2FA.`
          : `${count} active ${who}`;
      cy.get("[data-cy=mfa-unenrolled-local]", { timeout: 20000 }).should(
        "contain.text",
        expected(gaps.local, "local"),
      );
      cy.get("[data-cy=mfa-unenrolled-federated]").should(
        "contain.text",
        expected(gaps.federated, "federated"),
      );
    });
  });
});
