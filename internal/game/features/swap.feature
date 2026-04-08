Feature: Swap card
  A Swap card lets the drawer exchange one of their number cards with one of
  an opponent's number cards. Two-stage interaction: first choose a partner
  player, then choose which cards to exchange.

  Background:
    Given Alice has [3, 5]
    And Bob has [7, 9]
    And the deck is [Swap]

  Scenario: Swap exchanges chosen cards between two players
    When Alice draws
    Then a target choice is pending
    When Alice targets Bob
    Then a target choice is pending
    When Alice shuffles 3 for 7
    Then Alice has 7 in her hand
    And Alice does not have 3 in her hand
    And Bob has 3 in his hand
    And Bob does not have 7 in his hand
    And it is Bob's turn

  Scenario: Swap is discarded when drawer has no number cards
    Given Alice has []
    And Bob has [7]
    And the deck is [Swap]
    When Alice draws
    Then no target choice is pending
    And it is Bob's turn

  Scenario: Swap is discarded when no opponent has number cards
    Given Alice has [3]
    And Bob has []
    And the deck is [Swap]
    When Alice draws
    Then no target choice is pending
    And it is Bob's turn

  Scenario: Swapping for a card already held causes a bust
    Given Alice has [3, 5]
    And Bob has [5, 9]
    And the deck is [Swap]
    When Alice draws
    Then a target choice is pending
    When Alice targets Bob
    Then a target choice is pending
    When Alice shuffles 3 for 5
    Then Alice is busted
    And it is Bob's turn

  Scenario: Swap that gives the partner a card they already hold busts the partner
    Given Alice has [3, 7]
    And Bob has [3, 9]
    And the deck is [Swap]
    When Alice draws
    Then a target choice is pending
    When Alice targets Bob
    Then a target choice is pending
    When Alice shuffles 3 for 9
    Then Bob is busted
    And it is Alice's turn

