Feature: Thief card
  A Thief card lets the drawer steal one face-up number card from an opponent.
  Two-stage interaction: first choose a player, then choose which card to steal.

  Background:
    Given Alice has [3]
    And Bob has [5, 7]
    And the deck is [Thief]

  Scenario: Thief moves a card from victim to thief
    When Alice draws
    Then a target choice is pending
    When Alice targets Bob
    Then a target choice is pending
    When Alice steals 5
    Then Alice has 5 in her hand
    And Bob does not have 5 in his hand
    And it is Bob's turn

  Scenario: Thief is discarded when there are no stealable cards
    Given Alice has [5]
    And Bob has [5]
    And the deck is [Thief]
    When Alice draws
    Then no target choice is pending
    And it is Bob's turn

  Scenario: Stealing the seventh unique number triggers Flip 7
    Given Alice has [1, 2, 3, 4, 5, 6]
    And Bob has [7]
    And the deck is [Thief]
    When Alice draws
    And Alice targets Bob
    And Alice steals 7
    Then the round has ended
